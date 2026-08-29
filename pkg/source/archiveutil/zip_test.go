package archiveutil

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitPath(t *testing.T) {
	tests := []struct {
		path   string
		outer  string
		member string
		isZIP  bool
	}{
		{path: "release.zip!**/*.csv", outer: "release.zip", member: "**/*.csv", isZIP: true},
		{path: "release.ZIP!data/*.jsonl", outer: "release.ZIP", member: "data/*.jsonl", isZIP: true},
		{path: "release.zip", outer: "release.zip", member: DefaultMemberPattern, isZIP: true},
		{path: "release.zip!", outer: "release.zip", member: DefaultMemberPattern, isZIP: true},
		{path: "regular.csv", outer: "regular.csv"},
		{path: "object!name.csv", outer: "object!name.csv"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			outer, member, isZIP := SplitPath(tt.path)
			assert.Equal(t, tt.outer, outer)
			assert.Equal(t, tt.member, member)
			assert.Equal(t, tt.isZIP, isZIP)
		})
	}
}

func TestSelectZIPMembers(t *testing.T) {
	reader := makeZIPReader(t, map[string]string{
		"data/day-1.csv": "id\n1\n",
		"data/day-2.csv": "id\n2\n",
		"notes.txt":      "ignored",
	})

	members, err := SelectZIPMembers(reader, "data/*.csv", DefaultLimits())
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.ElementsMatch(t, []string{"data/day-1.csv", "data/day-2.csv"}, []string{members[0].Name, members[1].Name})

	_, err = SelectZIPMembers(reader, "missing/*.csv", DefaultLimits())
	require.ErrorContains(t, err, "no ZIP members matched")
}

func TestSelectZIPMembersRejectsUnsafePath(t *testing.T) {
	reader := makeZIPReader(t, map[string]string{"../escape.csv": "id\n1\n"})

	_, err := SelectZIPMembers(reader, "**/*.csv", DefaultLimits())
	require.ErrorContains(t, err, "unsafe ZIP member path")
}

func TestSelectZIPMembersEnforcesLimits(t *testing.T) {
	reader := makeZIPReader(t, map[string]string{
		"one.csv": "id\n1\n",
		"two.csv": "id\n2\n",
	})

	limits := DefaultLimits()
	limits.MaxMembers = 1
	_, err := SelectZIPMembers(reader, "*.csv", limits)
	require.ErrorContains(t, err, "exceeding the limit")

	limits = DefaultLimits()
	limits.MaxUncompressedBytes = 1
	_, err = SelectZIPMembers(reader, "*.csv", limits)
	require.ErrorContains(t, err, "uncompressed bytes")

	limits = DefaultLimits()
	limits.MaxExpansionRatio = 1
	reader.File[0].CompressedSize64 = 1
	_, err = SelectZIPMembers(reader, "*.csv", limits)
	require.ErrorContains(t, err, "expansion ratio")
}

func TestParseLimits(t *testing.T) {
	limits, err := ParseLimits(url.Values{
		"archive_max_members":            []string{"12"},
		"archive_max_bytes":              []string{"1024"},
		"archive_max_uncompressed_bytes": []string{"4096"},
		"archive_max_expansion_ratio":    []string{"25.5"},
	})
	require.NoError(t, err)
	assert.Equal(t, 12, limits.MaxMembers)
	assert.Equal(t, int64(1024), limits.MaxArchiveBytes)
	assert.Equal(t, uint64(4096), limits.MaxUncompressedBytes)
	assert.Equal(t, 25.5, limits.MaxExpansionRatio)

	for _, values := range []url.Values{
		{"archive_max_members": []string{"0"}},
		{"archive_max_bytes": []string{"invalid"}},
		{"archive_max_uncompressed_bytes": []string{"-1"}},
		{"archive_max_expansion_ratio": []string{"NaN"}},
	} {
		_, err := ParseLimits(values)
		require.Error(t, err)
	}
}

func TestForwardBatchesIgnoresErrorsAfterLimit(t *testing.T) {
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.AppendValues([]int64{1, 2}, nil)
	values := builder.NewArray()
	builder.Release()
	record := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{values}, 2)
	values.Release()

	batches := make(chan source.RecordBatchResult, 2)
	batches <- source.RecordBatchResult{Batch: record}
	batches <- source.RecordBatchResult{Err: errors.New("malformed row after limit")}
	close(batches)
	destination := make(chan source.RecordBatchResult, 1)

	rows, err := ForwardBatches(context.Background(), destination, batches, MemberMetadata{}, nil, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, rows)

	result := <-destination
	require.NoError(t, result.Err)
	assert.Equal(t, int64(1), result.Batch.NumRows())
	result.Batch.Release()
}

func makeZIPReader(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()

	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for name, contents := range files {
		member, err := writer.Create(name)
		require.NoError(t, err)
		_, err = member.Write([]byte(contents))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	reader, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	require.NoError(t, err)
	return reader
}
