package iceberg

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"strings"

	iceberg "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
)

func init() {
	// Hive Metastore canonicalizes local URIs as file:/absolute/path. The
	// upstream LocalFS only strips file://, which turns that URI into a relative
	// workspace path. Install a compatible file-scheme factory before catalogs
	// are used; plain paths still use iceberg-go's empty-scheme LocalFS.
	icebergio.Unregister("file")
	icebergio.Register("file", func(context.Context, *url.URL, map[string]string) (icebergio.IO, error) {
		return hiveLocalIO{}, nil
	})
}

type hiveFileCatalog struct {
	icebergcatalog.Catalog
}

func (c *hiveFileCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	return c.wrapTable(tbl), err
}

func (c *hiveFileCatalog) CreateTable(ctx context.Context, ident icebergtable.Identifier, tableSchema *iceberg.Schema, opts ...icebergcatalog.CreateTableOpt) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.CreateTable(ctx, ident, tableSchema, opts...)
	return c.wrapTable(tbl), err
}

func (c *hiveFileCatalog) RenameTable(ctx context.Context, from, to icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.RenameTable(ctx, from, to)
	return c.wrapTable(tbl), err
}

func (c *hiveFileCatalog) wrapTable(tbl *icebergtable.Table) *icebergtable.Table {
	if tbl == nil || !strings.HasPrefix(tbl.MetadataLocation(), "file:/") {
		return tbl
	}
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), func(context.Context) (icebergio.IO, error) {
		return hiveLocalIO{}, nil
	}, c)
}

func (c *hiveFileCatalog) Close() error {
	if closer, ok := c.Catalog.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type hiveLocalIO struct{ icebergio.LocalFS }

func normalizeHiveFilePath(path string) string {
	if strings.HasPrefix(path, "file://") {
		return strings.TrimPrefix(path, "file://")
	}
	return strings.TrimPrefix(path, "file:")
}

func (hiveLocalIO) Open(path string) (icebergio.File, error) {
	return icebergio.LocalFS{}.Open(normalizeHiveFilePath(path))
}

func (hiveLocalIO) ReadFile(path string) ([]byte, error) {
	return icebergio.LocalFS{}.ReadFile(normalizeHiveFilePath(path))
}

func (hiveLocalIO) Create(path string) (icebergio.FileWriter, error) {
	return icebergio.LocalFS{}.Create(normalizeHiveFilePath(path))
}

func (hiveLocalIO) WriteFile(path string, content []byte) error {
	return icebergio.LocalFS{}.WriteFile(normalizeHiveFilePath(path), content)
}

func (hiveLocalIO) Remove(path string) error {
	return icebergio.LocalFS{}.Remove(normalizeHiveFilePath(path))
}

func (hiveLocalIO) WalkDir(root string, fn fs.WalkDirFunc) error {
	return icebergio.LocalFS{}.WalkDir(normalizeHiveFilePath(root), fn)
}

func (hiveLocalIO) RemoveAll(path string) error {
	return icebergio.LocalFS{}.RemoveAll(normalizeHiveFilePath(path))
}

func (hiveLocalIO) ReadDir(path string) ([]fs.DirEntry, error) {
	return icebergio.LocalFS{}.ReadDir(normalizeHiveFilePath(path))
}

func (hiveLocalIO) MkdirAll(path string) error {
	return icebergio.LocalFS{}.MkdirAll(normalizeHiveFilePath(path))
}

func (hiveLocalIO) Mkdir(path string) error {
	return icebergio.LocalFS{}.Mkdir(normalizeHiveFilePath(path))
}

func (hiveLocalIO) Stat(path string) (fs.FileInfo, error) {
	return icebergio.LocalFS{}.Stat(normalizeHiveFilePath(path))
}

func (hiveLocalIO) Rename(oldPath, newPath string) error {
	return icebergio.LocalFS{}.Rename(normalizeHiveFilePath(oldPath), normalizeHiveFilePath(newPath))
}

func (hiveLocalIO) DeleteFiles(ctx context.Context, paths []string) ([]string, error) {
	deleted := make([]string, 0, len(paths))
	var deleteErr error
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return deleted, errors.Join(deleteErr, err)
		}
		if err := (hiveLocalIO{}).Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			deleteErr = errors.Join(deleteErr, err)
			continue
		}
		deleted = append(deleted, path)
	}
	return deleted, deleteErr
}
