package worldmem

import (
	"testing"

	"github.com/zeozeozeo/x3/db"
)

func TestPlanReindex(t *testing.T) {
	meta := db.WorldIndexMeta{Model: "model-a", Dim: 1024}

	cases := []struct {
		name      string
		modelName string
		meta      db.WorldIndexMeta
		metaOK    bool
		tableDim  int
		missing   int
		want      reindexPlan
	}{
		{
			name:      "healthy index needs no request",
			modelName: "model-a",
			meta:      meta,
			metaOK:    true,
			tableDim:  1024,
			missing:   0,
			want:      reindexPlan{Nothing: true, Dim: 1024},
		},
		{
			name:      "missing vectors reuse the recorded dimension",
			modelName: "model-a",
			meta:      meta,
			metaOK:    true,
			tableDim:  1024,
			missing:   7,
			want:      reindexPlan{Dim: 1024},
		},
		{
			name:      "absent vector table still reuses the recorded dimension",
			modelName: "model-a",
			meta:      meta,
			metaOK:    true,
			tableDim:  0,
			missing:   7,
			want:      reindexPlan{Dim: 1024},
		},
		{
			name:      "changed model must be probed",
			modelName: "model-b",
			meta:      meta,
			metaOK:    true,
			tableDim:  1024,
			missing:   0,
			want:      reindexPlan{Probe: true},
		},
		{
			name:      "database without metadata must be probed",
			modelName: "model-a",
			meta:      db.WorldIndexMeta{},
			metaOK:    false,
			tableDim:  768,
			missing:   0,
			want:      reindexPlan{Probe: true},
		},
		{
			name:      "recorded zero dimension is treated as unknown",
			modelName: "model-a",
			meta:      db.WorldIndexMeta{Model: "model-a", Dim: 0},
			metaOK:    true,
			tableDim:  0,
			missing:   0,
			want:      reindexPlan{Probe: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planReindex(tc.modelName, tc.meta, tc.metaOK, tc.tableDim, tc.missing)
			if got != tc.want {
				t.Fatalf("planReindex() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReindexIsNoOpWithoutEmbeddingAccounts(t *testing.T) {
	if EmbeddingsAvailable() {
		// A configured account would make this test talk to the network; the
		// behaviour under test is the unconfigured path.
		t.Skip("embedding accounts are configured in this environment")
	}
	n, err := Reindex(t.Context())
	if err != nil {
		t.Fatalf("Reindex() error = %v, want nil without accounts", err)
	}
	if n != 0 {
		t.Fatalf("Reindex() = %d, want 0 without accounts", n)
	}
}
