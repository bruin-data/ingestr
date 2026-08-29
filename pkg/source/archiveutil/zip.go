package archiveutil

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	DefaultMaxMembers                 = 10_000
	DefaultMaxArchiveBytes      int64 = 10 << 30
	DefaultMaxUncompressedBytes       = 100 << 30
	DefaultMaxExpansionRatio          = 1_000
	DefaultMemberPattern              = "**/*"
)

type Limits struct {
	MaxMembers           int
	MaxArchiveBytes      int64
	MaxUncompressedBytes uint64
	MaxExpansionRatio    float64
}

func DefaultLimits() Limits {
	return Limits{
		MaxMembers:           DefaultMaxMembers,
		MaxArchiveBytes:      DefaultMaxArchiveBytes,
		MaxUncompressedBytes: DefaultMaxUncompressedBytes,
		MaxExpansionRatio:    DefaultMaxExpansionRatio,
	}
}

func ParseLimits(values url.Values) (Limits, error) {
	limits := DefaultLimits()

	if value := values.Get("archive_max_members"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return Limits{}, fmt.Errorf("archive_max_members must be a positive integer")
		}
		limits.MaxMembers = parsed
	}
	if value := values.Get("archive_max_bytes"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 || parsed == math.MaxInt64 {
			return Limits{}, fmt.Errorf("archive_max_bytes must be a positive integer")
		}
		limits.MaxArchiveBytes = parsed
	}
	if value := values.Get("archive_max_uncompressed_bytes"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			return Limits{}, fmt.Errorf("archive_max_uncompressed_bytes must be a positive integer")
		}
		limits.MaxUncompressedBytes = parsed
	}
	if value := values.Get("archive_max_expansion_ratio"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed <= 0 || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return Limits{}, fmt.Errorf("archive_max_expansion_ratio must be a positive number")
		}
		limits.MaxExpansionRatio = parsed
	}

	return limits, nil
}

func ValidateArchiveSize(size int64, limits Limits) error {
	if size > limits.MaxArchiveBytes {
		return fmt.Errorf("ZIP archive is %d bytes, exceeding the limit of %d", size, limits.MaxArchiveBytes)
	}
	return nil
}

func ParseLimitsFromURI(uri string) (Limits, error) {
	queryStart := strings.IndexByte(uri, '?')
	if queryStart < 0 {
		return DefaultLimits(), nil
	}
	values, err := url.ParseQuery(uri[queryStart+1:])
	if err != nil {
		return Limits{}, fmt.Errorf("failed to parse archive limits: %w", err)
	}
	return ParseLimits(values)
}

func SplitPath(filePath string) (outerPath, memberPattern string, isZIP bool) {
	separator := strings.Index(filePath, "!")
	if separator >= 0 {
		outerPath = filePath[:separator]
		if !strings.HasSuffix(strings.ToLower(outerPath), ".zip") {
			return filePath, "", false
		}
		memberPattern = filePath[separator+1:]
		if memberPattern == "" {
			memberPattern = DefaultMemberPattern
		}
		return outerPath, memberPattern, true
	}

	if strings.HasSuffix(strings.ToLower(filePath), ".zip") {
		return filePath, DefaultMemberPattern, true
	}
	return filePath, "", false
}

func SelectZIPMembers(reader *zip.Reader, pattern string, limits Limits) ([]*zip.File, error) {
	if pattern == "" {
		pattern = DefaultMemberPattern
	}
	if _, err := doublestar.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("invalid ZIP member glob %q: %w", pattern, err)
	}
	if len(reader.File) > limits.MaxMembers {
		return nil, fmt.Errorf("ZIP archive has %d members, exceeding the limit of %d", len(reader.File), limits.MaxMembers)
	}

	matches := make([]*zip.File, 0)
	var compressedBytes uint64
	var uncompressedBytes uint64
	for _, member := range reader.File {
		memberPath, err := safeMemberPath(member.Name)
		if err != nil {
			return nil, err
		}
		if member.FileInfo().IsDir() {
			continue
		}

		matched, err := doublestar.Match(pattern, memberPath)
		if err != nil {
			return nil, fmt.Errorf("invalid ZIP member glob %q: %w", pattern, err)
		}
		if !matched {
			continue
		}

		if member.UncompressedSize64 > 0 && member.CompressedSize64 == 0 {
			return nil, fmt.Errorf("ZIP member %q exceeds the maximum expansion ratio of %g", member.Name, limits.MaxExpansionRatio)
		}
		if member.CompressedSize64 > 0 && float64(member.UncompressedSize64)/float64(member.CompressedSize64) > limits.MaxExpansionRatio {
			return nil, fmt.Errorf("ZIP member %q exceeds the maximum expansion ratio of %g", member.Name, limits.MaxExpansionRatio)
		}
		if math.MaxUint64-uncompressedBytes < member.UncompressedSize64 {
			return nil, fmt.Errorf("ZIP members exceed the maximum total uncompressed size of %d bytes", limits.MaxUncompressedBytes)
		}
		if math.MaxUint64-compressedBytes < member.CompressedSize64 {
			return nil, fmt.Errorf("ZIP member compressed sizes overflow the supported range")
		}
		uncompressedBytes += member.UncompressedSize64
		compressedBytes += member.CompressedSize64
		if uncompressedBytes > limits.MaxUncompressedBytes {
			return nil, fmt.Errorf("ZIP members total %d uncompressed bytes, exceeding the limit of %d", uncompressedBytes, limits.MaxUncompressedBytes)
		}
		matches = append(matches, member)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no ZIP members matched pattern: %s", pattern)
	}
	if compressedBytes > 0 && float64(uncompressedBytes)/float64(compressedBytes) > limits.MaxExpansionRatio {
		return nil, fmt.Errorf("ZIP members exceed the maximum expansion ratio of %g", limits.MaxExpansionRatio)
	}
	return matches, nil
}

func SpoolMember(ctx context.Context, member *zip.File) (*os.File, error) {
	if member.UncompressedSize64 >= math.MaxInt64 {
		return nil, fmt.Errorf("ZIP member %q is too large to spool", member.Name)
	}
	reader, err := member.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP member %q: %w", member.Name, err)
	}
	defer func() { _ = reader.Close() }()

	tempFile, err := os.CreateTemp("", "ingestr-zip-member-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create ZIP member spool file: %w", err)
	}
	cleanup := func() {
		name := tempFile.Name()
		_ = tempFile.Close()
		_ = os.Remove(name)
	}

	written, err := io.Copy(tempFile, io.LimitReader(&contextReader{ctx: ctx, reader: reader}, int64(member.UncompressedSize64)+1))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to spool ZIP member %q: %w", member.Name, err)
	}
	if written > int64(member.UncompressedSize64) {
		cleanup()
		return nil, fmt.Errorf("ZIP member %q exceeds its declared uncompressed size", member.Name)
	}
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to rewind ZIP member %q: %w", member.Name, err)
	}
	return tempFile, nil
}

func ForwardBatches(ctx context.Context, destination chan<- source.RecordBatchResult, batches <-chan source.RecordBatchResult, limit int) (int, error) {
	rows := 0
	var firstErr error
	for result := range batches {
		if result.Err != nil {
			if firstErr == nil && (limit <= 0 || rows < limit) {
				firstErr = result.Err
			}
			continue
		}
		if result.Batch == nil {
			continue
		}
		if firstErr != nil || (limit > 0 && rows >= limit) {
			result.Batch.Release()
			continue
		}

		batch := result.Batch
		if limit > 0 && rows+int(batch.NumRows()) > limit {
			sliced := batch.NewSlice(0, int64(limit-rows))
			batch.Release()
			batch = sliced
		}

		select {
		case destination <- source.RecordBatchResult{Batch: batch}:
			rows += int(batch.NumRows())
		case <-ctx.Done():
			batch.Release()
			firstErr = ctx.Err()
		}
	}
	return rows, firstErr
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}

func safeMemberPath(name string) (string, error) {
	normalized := strings.ReplaceAll(name, `\`, "/")
	cleaned := path.Clean(normalized)
	firstPart := cleaned
	if separator := strings.IndexByte(firstPart, '/'); separator >= 0 {
		firstPart = firstPart[:separator]
	}
	if normalized == "" || strings.HasPrefix(normalized, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(firstPart, ":") {
		return "", fmt.Errorf("unsafe ZIP member path %q", name)
	}
	return cleaned, nil
}
