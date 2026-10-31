package worldmem

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/model"
)

// reindexBatch bounds how many facts are embedded per request.
const reindexBatch = 32

// reindexProbeText is embedded once to discover the configured model's vector
// width. It is only used when the width is not already known.
const reindexProbeText = "x3 world memory index probe"

var reindexMu sync.Mutex

// reindexPlan is what Reindex must do, decided without any I/O so it can be
// unit tested.
type reindexPlan struct {
	// Nothing is set when the index already matches the configured model and no
	// fact is missing a vector. Reindex then makes no request at all.
	Nothing bool
	// Probe is set when the configured model's vector width is unknown, so it
	// has to be discovered by embedding something.
	Probe bool
	// Dim is the known vector width, valid when Probe is false.
	Dim int
}

// planReindex decides whether the index needs rebuilding or repairs, and
// whether the configured model's dimension still has to be discovered.
//
// Requiring a request on every startup just to re-confirm a healthy index would
// be wasteful: the model that produced the stored vectors is recorded, so the
// only times a request is genuinely needed are a database that predates the
// bookkeeping, or a model whose name has actually changed.
func planReindex(modelName string, meta db.WorldIndexMeta, metaOK bool, tableDim, missing int) reindexPlan {
	dimKnown := metaOK && meta.Model == modelName && meta.Dim > 0
	switch {
	case dimKnown && meta.Dim == tableDim && missing == 0:
		return reindexPlan{Nothing: true, Dim: meta.Dim}
	case dimKnown:
		return reindexPlan{Dim: meta.Dim}
	default:
		return reindexPlan{Probe: true}
	}
}

// Reindex repairs the vector index so every stored fact can be retrieved again.
func Reindex(ctx context.Context) (int, error) {
	reindexMu.Lock()
	defer reindexMu.Unlock()

	if db.WorldVecUsesLegacyPartition() {
		tableDim := db.CurrentWorldVecDim()
		if tableDim > 0 {
			if err := db.MigrateWorldVecPartitionSchema(tableDim); err != nil {
				return 0, fmt.Errorf("worldmem reindex: migrating vector table: %w", err)
			}
			db.Vacuum()
		}
	}

	if !EmbeddingsAvailable() {
		slog.Info("worldmem reindex skipped: no embedding accounts configured")
		return 0, nil
	}

	total, err := db.CountAllWorldEntries()
	if err != nil {
		return 0, fmt.Errorf("worldmem reindex: counting facts: %w", err)
	}
	if total == 0 {
		return 0, nil
	}

	modelName := strings.TrimSpace(model.WorldMemEmbeddingModel)
	if modelName == "" {
		modelName = model.DefaultWorldMemEmbeddingModel
	}

	meta, metaOK, err := db.GetWorldIndexMeta()
	if err != nil {
		return 0, fmt.Errorf("worldmem reindex: reading index metadata: %w", err)
	}
	tableDim := db.CurrentWorldVecDim()
	missing, err := db.CountWorldEntriesWithoutVectors()
	if err != nil {
		return 0, fmt.Errorf("worldmem reindex: counting facts without vectors: %w", err)
	}

	plan := planReindex(modelName, meta, metaOK, tableDim, missing)
	// The healthy path: the index already matches the configured model and
	// nothing is missing. Do not spend an embedding request confirming it.
	if plan.Nothing {
		return 0, nil
	}

	dim := plan.Dim
	if plan.Probe {
		probe, err := EmbedOne(ctx, reindexProbeText)
		if err != nil {
			return 0, fmt.Errorf("worldmem reindex: embedding probe: %w", err)
		}
		if len(probe) == 0 {
			return 0, fmt.Errorf("worldmem reindex: embedding probe returned an empty vector")
		}
		dim = len(probe)
	}

	rebuilt := false
	if dim != tableDim {
		slog.Warn("worldmem reindex: embedding dimension changed, rebuilding vector index",
			"stored_dim", tableDim, "configured_dim", dim, "model", modelName, "facts", total)
		if err := db.RebuildWorldVecTable(dim); err != nil {
			return 0, fmt.Errorf("worldmem reindex: rebuilding vector table: %w", err)
		}
		rebuilt = true
	}

	reembedded := 0
	for {
		pending, err := db.WorldEntriesMissingVectors(reindexBatch)
		if err != nil {
			return reembedded, fmt.Errorf("worldmem reindex: listing facts without vectors: %w", err)
		}
		if len(pending) == 0 {
			break
		}

		texts := make([]string, len(pending))
		for i, e := range pending {
			texts[i] = e.Text
		}
		vecs, err := Embed(ctx, texts)
		if err != nil {
			return reembedded, fmt.Errorf("worldmem reindex: embedding facts: %w", err)
		}

		stored := 0
		for i, e := range pending {
			if i >= len(vecs) || len(vecs[i]) == 0 {
				continue
			}
			if err := db.UpsertWorldVector(e.ID, e.ChannelKey, vecs[i], dim); err != nil {
				slog.Warn("worldmem reindex: storing vector failed",
					"id", e.ID, "channel", e.ChannelKey, "err", err)
				continue
			}
			stored++
		}
		reembedded += stored
		if stored == 0 {
			// No progress: stop rather than spin on the same rows forever.
			return reembedded, fmt.Errorf("worldmem reindex: could not store any of %d pending vectors", len(pending))
		}
	}

	// Record what produced this index so the next startup needs no request.
	if err := db.SetWorldIndexMeta(db.WorldIndexMeta{Model: modelName, Dim: dim}); err != nil {
		slog.Warn("worldmem reindex: recording index metadata failed", "err", err)
	}

	if reembedded > 0 {
		slog.Info("worldmem reindex: re-embedded facts", "count", reembedded, "model", modelName, "dim", dim)
	}
	if rebuilt {
		db.Vacuum()
	}
	return reembedded, nil
}
