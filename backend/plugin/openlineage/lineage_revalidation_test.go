package openlineage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// A pass queued behind the held semaphore must observe its context: shutdown
// cannot be reduced to waiting for whatever pass holds it, and a pass timeout has
// to bound the wait rather than only the queries run after acquiring.
func TestRevalidationWaitIsCancellable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(*Processor, context.Context) error
	}{
		{
			name: "unresolved lineage",
			call: func(processor *Processor, ctx context.Context) error {
				_, _, _, err := processor.RevalidateUnresolvedLineage(ctx, 0)
				return err
			},
		},
		{
			name: "contradicted column claims",
			call: func(processor *Processor, ctx context.Context) error {
				_, _, _, err := processor.RevalidateContradictedColumnClaims(ctx, 0)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Acquiring the semaphore is the first thing either method does, so a
			// held semaphore is observable without a store.
			processor := &Processor{revalidateSem: make(chan struct{}, 1)}
			processor.revalidateSem <- struct{}{}

			ctx, cancel := context.WithCancel(context.Background())
			errs := make(chan error, 1)
			go func() { errs <- tt.call(processor, ctx) }()

			cancel()
			select {
			case err := <-errs:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(5 * time.Second):
				t.Fatal("a queued revalidation pass did not return when its context was cancelled")
			}
		})
	}
}

// NewProcessor must own the semaphore: a pass queued before the first one is
// released would otherwise block on a nil channel (or never observe ctx).
func TestNewProcessorInitializesTheRevalidationSemaphore(t *testing.T) {
	t.Parallel()

	processor := NewProcessor(nil, nil, nil)
	require.NotNil(t, processor.revalidateSem)
	require.Equal(t, 1, cap(processor.revalidateSem))
}

// A run whose validated edge set equals the stored one must not be rewritten:
// still-unknown edges validate to themselves, and rewriting them on every pass is
// pure write amplification. The comparison has to include the transformation and
// ignore only the row identity the replace rewrites.
func TestSameLineageEdges(t *testing.T) {
	t.Parallel()

	edge := func(source, target, transformation string) *store.ColumnLineage {
		lineage := &store.ColumnLineage{
			SourceGUID:     source,
			SourceColumn:   "c",
			TargetGUID:     target,
			TargetColumn:   "t",
			RelationType:   model.RelationType(1),
			Transformation: []model.Transformation{{Expression: transformation}},
		}
		return lineage
	}

	require.True(t, sameLineageEdges(nil, nil))
	require.True(t, sameLineageEdges(
		[]*store.ColumnLineage{edge("s", "t", "a")},
		[]*store.ColumnLineage{edge("s", "t", "a")},
	))
	require.True(t, sameLineageEdges(
		[]*store.ColumnLineage{edge("s", "t", "a"), edge("s2", "t2", "b")},
		[]*store.ColumnLineage{edge("s2", "t2", "b"), edge("s", "t", "a")},
	), "edge order is not part of the set")
	require.False(t, sameLineageEdges(
		[]*store.ColumnLineage{edge("s", "t", "a")},
		[]*store.ColumnLineage{edge("s", "t", "b")},
	), "a different transformation is a different edge")
	require.False(t, sameLineageEdges(
		[]*store.ColumnLineage{edge("s", "t", "a")},
		[]*store.ColumnLineage{edge("s", "t2", "a")},
	))
	require.False(t, sameLineageEdges(
		[]*store.ColumnLineage{edge("s", "t", "a")},
		[]*store.ColumnLineage{edge("s", "t", "a"), edge("s2", "t2", "b")},
	))
}
