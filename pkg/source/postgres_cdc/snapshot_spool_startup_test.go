package postgres_cdc

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestCompletedSnapshotSpoolPositions(t *testing.T) {
	for _, tt := range []struct {
		name         string
		configure    func(*snapshotSpoolReader, *snapshotSpoolState, map[string]string)
		want         map[string]string
		sameIdentity bool
	}{
		{
			name:         "matching baseline supplies missing position",
			want:         map[string]string{"public.items": FormatLSN(20)},
			sameIdentity: true,
		},
		{
			name: "managed position takes precedence",
			configure: func(_ *snapshotSpoolReader, _ *snapshotSpoolState, resumes map[string]string) {
				resumes["public.items"] = FormatLSN(80)
			},
			want:         map[string]string{"public.items": FormatLSN(80)},
			sameIdentity: true,
		},
		{
			name: "schema evolution uses managed position",
			configure: func(r *snapshotSpoolReader, _ *snapshotSpoolState, resumes map[string]string) {
				r.tables[0].SchemaFingerprint = "evolved-schema"
				r.opts.CDCResumeSchemaFingerprint = "evolved-schema"
				resumes["public.items"] = FormatLSN(80)
			},
			want: map[string]string{"public.items": FormatLSN(80)},
		},
		{
			name: "schema evolution needs a managed position",
			configure: func(r *snapshotSpoolReader, _ *snapshotSpoolState, _ map[string]string) {
				r.tables[0].SchemaFingerprint = "evolved-schema"
			},
			want: map[string]string{"public.items": ""},
		},
		{
			name: "changed destination cannot reuse baseline",
			configure: func(_ *snapshotSpoolReader, state *snapshotSpoolState, _ map[string]string) {
				state.Identity = "replaced-destination"
			},
			want: map[string]string{"public.items": ""},
		},
		{
			name: "changed source incarnation cannot reuse baseline",
			configure: func(r *snapshotSpoolReader, _ *snapshotSpoolState, _ map[string]string) {
				r.tables[0].Incarnation = "43"
			},
			want: map[string]string{"public.items": ""},
		},
		{
			name: "changed slot cannot reuse baseline",
			configure: func(_ *snapshotSpoolReader, state *snapshotSpoolState, _ map[string]string) {
				state.Slot = "other-slot"
			},
			want: map[string]string{"public.items": ""},
		},
		{
			name: "newly selected table needs its own baseline",
			configure: func(r *snapshotSpoolReader, _ *snapshotSpoolState, resumes map[string]string) {
				r.multi = true
				r.tables = append(r.tables, source.SourceTableInfo{Name: "public.new_items", Incarnation: "43", SchemaFingerprint: "new-schema"})
				resumes["public.items"] = FormatLSN(80)
			},
			want: map[string]string{"public.items": FormatLSN(80), "public.new_items": ""},
		},
		{
			name: "removed table prevents aggregate baseline fallback",
			configure: func(r *snapshotSpoolReader, state *snapshotSpoolState, _ map[string]string) {
				r.multi = true
				state.Tables["public.old_items"] = state.Tables["public.items"]
			},
			want: map[string]string{"public.items": ""},
		},
		{
			name: "invalid managed schema cannot fall back to matching spool",
			configure: func(r *snapshotSpoolReader, _ *snapshotSpoolState, resumes map[string]string) {
				r.opts.CDCResumeSchemaFingerprint = "stale-schema"
				resumes["public.items"] = FormatLSN(80)
			},
			want: map[string]string{"public.items": ""},
		},
		{
			name: "invalid managed incarnation cannot fall back to matching spool",
			configure: func(r *snapshotSpoolReader, _ *snapshotSpoolState, resumes map[string]string) {
				r.opts.CDCResumeIncarnation = "41"
				resumes["public.items"] = FormatLSN(80)
			},
			want: map[string]string{"public.items": ""},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newChunkTestStore()
			state := snapshotSpoolTestState(store.identity)
			state.CopyComplete, state.Complete = true, true
			state.Retired = []snapshotSpoolRetired{{Epoch: "retired", Identity: store.identity, Mode: "destination"}}
			r := &snapshotSpoolReader{
				tables:  []source.SourceTableInfo{{Name: "public.items", Incarnation: "42", SchemaFingerprint: "schema"}},
				options: &source.CDCSnapshotSpoolOptions{Store: store},
			}
			resumes := make(map[string]string)
			if tt.configure != nil {
				tt.configure(r, state, resumes)
			}
			originalState := state.clone()
			originalResumes := make(map[string]string, len(resumes))
			for name, position := range resumes {
				originalResumes[name] = position
			}
			positions, sameIdentity, err := r.completedSpoolPositions(t.Context(), state, "slot", resumes)
			require.NoError(t, err)
			require.Equal(t, tt.want, positions)
			require.Equal(t, tt.sameIdentity, sameIdentity)
			require.Equal(t, originalState, state, "completed metadata must remain available for retirement")
			require.Equal(t, originalResumes, resumes)
		})
	}
}
