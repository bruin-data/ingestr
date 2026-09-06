package gliner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	checkpoint = "9b7f39b0a2da971a5beea78d35f1539d4009c891"
	modelURL   = "https://huggingface.co/knowledgator/gliner-pii-edge-v1.0/resolve/" + checkpoint
)

var assets = []struct{ name, checksum string }{
	{"onnx/model.onnx", "4ca588722e6d79447ad4c9c230eeba3d9d472c672a9598184a34e9f77fc35836"},
	{"tokenizer.json", "84b3a9b18f04a0ccd03b72d9f871b7e0bec40fd7021ef50bc30a7c3693c11205"},
	{"tokenizer_config.json", "3398f6d1ad4b4c4f9874d390d060a75c58cad5e5ce9b22841b3e40643b4ada27"},
}

// An explicit directory is offline-only; the default cache downloads missing assets.
func modelDirectory(ctx context.Context) (string, error) {
	dir := os.Getenv("INGESTR_GLINER_MODEL_DIR")
	download := dir == ""
	if download {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("find GLiNER cache: %w", err)
		}
		dir = filepath.Join(cache, "ingestr", "models", "gliner-pii-edge", checkpoint)
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	for _, asset := range assets {
		if err := ensureAsset(ctx, client, modelURL, dir, asset.name, asset.checksum, download); err != nil {
			return "", fmt.Errorf("GLiNER asset %s: %w", asset.name, err)
		}
	}
	return dir, nil
}

func ensureAsset(ctx context.Context, client *http.Client, baseURL, dir, name, checksum string, download bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(dir, filepath.FromSlash(name))
	f, err := os.Open(path)
	if err == nil {
		defer func() { _ = f.Close() }()
		return verifyAsset(f, checksum)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("open cached file: %w", err)
	}
	if !download {
		return fmt.Errorf("missing file in INGESTR_GLINER_MODEL_DIR (offline mode)")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+name, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	// Unique temporary files and atomic publication also allow concurrent processes
	// to share a cache without ever reading partial weights.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gliner-*")
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	defer func() { _ = tmp.Close() }()
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 256<<20)); err != nil {
		return fmt.Errorf("save download: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind download: %w", err)
	}
	if err := verifyAsset(tmp, checksum); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close download: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("publish download: %w", err)
	}
	return nil
}

func verifyAsset(r io.Reader, checksum string) error {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return fmt.Errorf("read checksum: %w", err)
	}
	if fmt.Sprintf("%x", h.Sum(nil)) != checksum {
		return fmt.Errorf("checksum mismatch; remove the corrupt file and retry, or supply the pinned model assets")
	}
	return nil
}
