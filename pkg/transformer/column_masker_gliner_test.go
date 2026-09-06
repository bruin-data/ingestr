package transformer

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

type fakePIIMasker struct {
	calls, closes int
	err           error
}

func (f *fakePIIMasker) Mask(string) (string, error) {
	f.calls++
	return "<NAME>", f.err
}

func (f *fakePIIMasker) Close() { f.closes++ }

func TestGLinerOptIn(t *testing.T) {
	// A broken model location must not affect any pre-existing algorithm, even
	// when a gliner setting was overridden by a later setting for the same field.
	t.Setenv("INGESTR_GLINER_MODEL_DIR", filepath.Join(t.TempDir(), "missing"))
	for name := range algorithms {
		if name == "gliner" {
			continue
		}
		cfg := "x:" + name
		if name == "hmac" {
			cfg += ":key"
		}
		m := mustMasker(t, "x:gliner", cfg)
		m.newGLiner = func(context.Context) (piiMasker, error) {
			t.Fatal("non-model path initialized GLiNER")
			return nil, nil
		}
		require.NoError(t, m.Prepare(t.Context()))
		_, _, err := algorithms[name].apply(m, "example", arrow.BinaryTypes.String, "key", name == "hmac")
		require.NoError(t, err)
		m.Close()
	}
	m := mustMasker(t)
	require.NoError(t, m.Prepare(t.Context()))
	m.Close()
	m = mustMasker(t, "x:GLINER")
	require.NotNil(t, m.OutputSchema(arrow.NewSchema([]arrow.Field{{Name: "x", Type: arrow.PrimitiveTypes.Int64}}, nil)))
	require.Nil(t, m.gliner)
	require.ErrorContains(t, m.Prepare(t.Context()), "offline mode")
	m.Close()
	_, err := NewColumnMasker([]string{"x:gliner:0.5"})
	require.ErrorContains(t, err, "does not accept a parameter")
}

func TestGLinerSharedAcrossFieldsRowsAndConcurrentBatches(t *testing.T) {
	m := mustMasker(t, "x:gliner", "y:gliner")
	fake := &fakePIIMasker{}
	loads := 0
	m.newGLiner = func(context.Context) (piiMasker, error) {
		loads++
		return fake, nil
	}
	pool := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer pool.AssertSize(t, 0)
	b := array.NewStringBuilder(pool)
	b.AppendValues([]string{"Ada", "", "ignored", "Grace"}, []bool{true, true, false, true})
	col := b.NewArray()
	b.Release()
	defer col.Release()
	s := arrow.NewSchema([]arrow.Field{{Name: "x", Type: arrow.BinaryTypes.String}, {Name: "y", Type: arrow.BinaryTypes.String}}, nil)
	batch := array.NewRecordBatch(s, []arrow.Array{col, col}, 4)
	defer batch.Release()
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			out, err := m.Transform(batch)
			if err != nil {
				t.Error(err)
				return
			}
			defer out.Release()
			for _, c := range out.Columns() {
				if c.ValueStr(0) != "<NAME>" || c.ValueStr(1) != "" || !c.IsNull(2) || c.ValueStr(3) != "<NAME>" {
					t.Error("unexpected masked values")
				}
			}
		})
	}
	wg.Wait()
	require.Equal(t, 1, loads)
	require.Equal(t, 12, fake.calls)
	fake.err = fmt.Errorf("GLiNER inference failed")
	out, err := m.Transform(batch)
	require.Nil(t, out)
	require.ErrorContains(t, err, `mask "gliner" on column "x"`)
	require.NotContains(t, err.Error(), "Ada")
	m.Close()
	m.Close()
	require.Equal(t, 1, fake.closes)
	require.ErrorContains(t, m.Prepare(t.Context()), "closed")
}

func TestGLinerCancellationBetweenValues(t *testing.T) {
	m := mustMasker(t, "x:gliner")
	defer m.Close()
	fake := &fakePIIMasker{}
	loads := 0
	m.newGLiner = func(context.Context) (piiMasker, error) {
		loads++
		return fake, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, m.Prepare(ctx))
	require.NoError(t, m.Prepare(ctx))
	require.Equal(t, 1, loads)
	cancel()
	_, _, err := algoGLiner(m, "text", arrow.BinaryTypes.String, "", false)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, fake.calls)
}
