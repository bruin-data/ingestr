package bigquery

import (
	"io"
	"sync"

	"github.com/klauspost/compress/gzip"
	"google.golang.org/grpc/encoding"
)

func init() {
	// Override the default gzip compressor with level 3 for faster compression.
	// Level 3 is ~2x faster than level 6 with only ~5% larger output.
	encoding.RegisterCompressor(&fastGzipCompressor{})
}

type fastGzipCompressor struct{}

func (c *fastGzipCompressor) Name() string { return "gzip" }

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(io.Discard, 3)
		return w
	},
}

func (c *fastGzipCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	gz := gzipWriterPool.Get().(*gzip.Writer)
	gz.Reset(w)
	return &pooledGzipWriter{Writer: gz}, nil
}

type pooledGzipWriter struct {
	*gzip.Writer
}

func (w *pooledGzipWriter) Close() error {
	err := w.Writer.Close()
	gzipWriterPool.Put(w.Writer)
	return err
}

func (c *fastGzipCompressor) Decompress(r io.Reader) (io.Reader, error) {
	return gzip.NewReader(r)
}
