package gliner

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gomlx/compute/gobackend"
	"github.com/stretchr/testify/require"
)

type reference struct {
	Text     string
	Inputs   map[string][][]int64
	Logits   []float32
	Entities []entity
}

func readReference(t *testing.T, path string) reference {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var f reference
	require.NoError(t, json.Unmarshal(data, &f))
	return f
}

func fixtureWords(text string) []word {
	var words []word
	for _, match := range wordPattern.FindAllStringIndex(text, -1) {
		words = append(words, word{match[0], match[1]})
	}
	return words
}

func TestDecodeReferenceFixtures(t *testing.T) {
	paths, err := filepath.Glob("testdata/*.json")
	require.NoError(t, err)
	require.Len(t, paths, 4)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f := readReference(t, path)
			entities, masked, err := decode(f.Text, fixtureWords(f.Text), f.Logits, 0.3)
			require.NoError(t, err)
			checkEntities(t, entities, f.Entities)
			for _, e := range entities {
				require.NotContains(t, masked, e.Text)
			}
		})
	}
}

func checkEntities(t *testing.T, got, want []entity) {
	t.Helper()
	require.Len(t, got, len(want))
	for i, e := range got {
		require.Equal(t, want[i].Start, e.Start)
		require.Equal(t, want[i].End, e.End)
		require.Equal(t, want[i].Label, e.Label)
	}
}

func TestRejectInvalidInputAndLogits(t *testing.T) {
	for _, text := range []string{"", "\xff", strings.Repeat("word ", 2049)} {
		_, _, err := tokenize(nil, text)
		require.Error(t, err)
	}
	_, _, err := decode("word", []word{{0, 4}}, nil, 0.3)
	require.Error(t, err)
	logits := make([]float32, len(labels)*3)
	logits[0] = float32(math.NaN())
	_, _, err = decode("word", []word{{0, 4}}, logits, 0.3)
	require.ErrorContains(t, err, "non-finite")
}

func TestInferencePanicDoesNotExposeInput(t *testing.T) {
	b, err := gobackend.New("")
	require.NoError(t, err)
	r := &Runtime{backend: b} // Missing tokenizer forces a library panic.
	defer r.Close()
	masked, err := r.Mask("sensitive-input@example.com")
	require.Empty(t, masked)
	require.EqualError(t, err, "GLiNER inference failed")
}

// This is the only test allowed to download the real weights, and is explicitly
// gated even outside -short. Standard tests run entirely offline.
func TestModelReferenceParity(t *testing.T) {
	if os.Getenv("INGESTR_TEST_GLINER") != "1" {
		t.Skip("set INGESTR_TEST_GLINER=1 to download/load the pinned FP32 model")
	}
	r, err := New(t.Context())
	require.NoError(t, err)
	defer r.Close()
	paths, err := filepath.Glob("testdata/fp32-*.json")
	require.NoError(t, err)
	// Revisit the first input after different texts to catch stale constants.
	paths = append(paths, paths[0])
	for _, path := range paths {
		f := readReference(t, path)
		inputs, words, err := tokenize(r.tok, f.Text)
		require.NoError(t, err)
		require.Equal(t, f.Inputs, inputs)
		logits, err := r.infer(inputs)
		require.NoError(t, err)
		require.Len(t, logits, len(f.Logits))
		maxDiff := 0.0
		for i, value := range logits {
			diff := math.Abs(float64(value - f.Logits[i]))
			require.False(t, math.IsNaN(diff) || math.IsInf(diff, 0))
			maxDiff = math.Max(maxDiff, diff)
		}
		t.Logf("%s max logit error %.8f", path, maxDiff)
		require.Less(t, maxDiff, 1e-3)
		entities, _, err := decode(f.Text, words, logits, 0.3)
		require.NoError(t, err)
		checkEntities(t, entities, f.Entities)
	}
	var wg sync.WaitGroup
	for _, path := range paths[:3] {
		f := readReference(t, path)
		_, want, err := decode(f.Text, fixtureWords(f.Text), f.Logits, 0.3)
		require.NoError(t, err)
		wg.Go(func() {
			got, err := r.Mask(f.Text)
			if err != nil || got != want {
				t.Errorf("concurrent masking failed for fixture %s", path)
			}
		})
	}
	wg.Wait()
	r.Close()
	_, err = r.Mask("text")
	require.ErrorContains(t, err, "closed")
}
