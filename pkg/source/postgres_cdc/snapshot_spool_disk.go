package postgres_cdc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	snapshotDiskMagic      = "INGWAL"
	snapshotDiskVersion    = byte(1)
	snapshotDiskHeaderSize = len(snapshotDiskMagic) + 1 + 2*sha256.Size
)

type snapshotDiskStore struct {
	directory string
	identity  string
	scope     [sha256.Size]byte
}

func newSnapshotDiskStore(directory, identity string) (source.CDCChunkStore, error) {
	if directory == "" || identity == "" {
		return nil, fmt.Errorf("snapshot WAL disk storage requires a directory and identity")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve snapshot WAL directory: %w", err)
	}
	scope := sha256.Sum256([]byte(identity))
	store := &snapshotDiskStore{
		directory: filepath.Join(absolute, hex.EncodeToString(scope[:])),
		identity:  identity,
		scope:     scope,
	}
	if err := makeSnapshotDiskDirectory(store.directory); err != nil {
		return nil, fmt.Errorf("create snapshot WAL directory: %w", err)
	}
	info, err := os.Lstat(store.directory)
	if err != nil {
		return nil, fmt.Errorf("inspect snapshot WAL directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("snapshot WAL identity path must be a directory")
	}
	if err := os.Chmod(store.directory, 0o700); err != nil {
		return nil, fmt.Errorf("secure snapshot WAL directory: %w", err)
	}
	if err := syncSnapshotDiskDirectory(store.directory); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *snapshotDiskStore) path(key string) string {
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(s.directory, hex.EncodeToString(digest[:])+".wal")
}

func (s *snapshotDiskStore) binding(key string) [sha256.Size]byte {
	keyHash := sha256.Sum256([]byte(key))
	var material [2 * sha256.Size]byte
	copy(material[:sha256.Size], s.scope[:])
	copy(material[sha256.Size:], keyHash[:])
	return sha256.Sum256(material[:])
}

func (s *snapshotDiskStore) Load(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	encoded, err := os.ReadFile(s.path(key))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read snapshot WAL key %q: %w", key, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(encoded) < snapshotDiskHeaderSize || string(encoded[:len(snapshotDiskMagic)]) != snapshotDiskMagic {
		return nil, fmt.Errorf("invalid snapshot WAL file for key %q", key)
	}
	if encoded[len(snapshotDiskMagic)] != snapshotDiskVersion {
		return nil, fmt.Errorf("unsupported snapshot WAL version for key %q", key)
	}
	start := len(snapshotDiskMagic) + 1
	binding := s.binding(key)
	if !bytes.Equal(encoded[start:start+sha256.Size], binding[:]) {
		return nil, fmt.Errorf("snapshot WAL identity or key mismatch for %q", key)
	}
	payload := encoded[snapshotDiskHeaderSize:]
	checksum := sha256.Sum256(payload)
	if !bytes.Equal(encoded[start+sha256.Size:snapshotDiskHeaderSize], checksum[:]) {
		return nil, fmt.Errorf("snapshot WAL checksum mismatch for key %q", key)
	}
	return payload, nil
}

func (s *snapshotDiskStore) Save(ctx context.Context, key string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.CreateTemp(s.directory, ".wal-*")
	if err != nil {
		return fmt.Errorf("create snapshot WAL file: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	var header [snapshotDiskHeaderSize]byte
	copy(header[:], snapshotDiskMagic)
	header[len(snapshotDiskMagic)] = snapshotDiskVersion
	start := len(snapshotDiskMagic) + 1
	binding := s.binding(key)
	copy(header[start:], binding[:])
	checksum := sha256.Sum256(payload)
	copy(header[start+sha256.Size:], checksum[:])
	if _, err := file.Write(header[:]); err != nil {
		return fmt.Errorf("write snapshot WAL header: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("write snapshot WAL payload: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync snapshot WAL file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close snapshot WAL file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), s.path(key)); err != nil {
		return fmt.Errorf("publish snapshot WAL key %q: %w", key, err)
	}
	return syncSnapshotDiskDirectory(s.directory)
}

func (s *snapshotDiskStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(s.path(key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete snapshot WAL key %q: %w", key, err)
	}
	return syncSnapshotDiskDirectory(s.directory)
}

func (s *snapshotDiskStore) Identity(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return s.identity, nil
}

func makeSnapshotDiskDirectory(directory string) error {
	info, err := os.Stat(directory)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("snapshot WAL path %q is not a directory", directory)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(directory)
	if err := makeSnapshotDiskDirectory(parent); err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	return syncSnapshotDiskDirectory(parent)
}

func syncSnapshotDiskDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open snapshot WAL directory for sync: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync snapshot WAL directory: %w", err)
	}
	return nil
}
