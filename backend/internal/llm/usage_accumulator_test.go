package llm

import (
	"context"
	"testing"
)

// A cost already rolled up to the user's totals elsewhere (the RAG query
// embedding) belongs in the turn's figure, the thread's Σ, but not a second
// time in the lifetime rollup, which reads Cost.
func TestUsageAccumulator_RolledUpCostCountsForTheTurnOnly(t *testing.T) {
	acc := NewUsageAccumulator()
	ctx := WithUsageAccumulator(context.Background(), acc)

	RecordCost(ctx, 1000, true)
	RecordRolledUpCost(ctx, 20)

	if nano, priced := acc.Cost(); nano != 1000 || !priced {
		t.Fatalf("Cost() = %d/%v, want 1000/true", nano, priced)
	}
	if nano, priced := acc.TurnCost(); nano != 1020 || !priced {
		t.Fatalf("TurnCost() = %d/%v, want 1020/true", nano, priced)
	}
}

func TestUsageAccumulator_RolledUpCostAlonePricesTheTurn(t *testing.T) {
	acc := NewUsageAccumulator()
	RecordRolledUpCost(WithUsageAccumulator(context.Background(), acc), 20)

	if _, priced := acc.Cost(); priced {
		t.Fatal("Cost() priced with no chat call recorded")
	}
	if nano, priced := acc.TurnCost(); nano != 20 || !priced {
		t.Fatalf("TurnCost() = %d/%v, want 20/true", nano, priced)
	}
}

func TestRecordRolledUpCost_NoAccumulatorIsANoOp(t *testing.T) {
	RecordRolledUpCost(context.Background(), 20)
}
