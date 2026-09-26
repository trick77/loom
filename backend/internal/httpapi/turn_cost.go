package httpapi

import (
	"context"
	"log/slog"
	"sync"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
	"github.com/trick77/loom/internal/usage"
)

// turnCostSettler books everything a streamed turn spent, whichever way the
// turn ends. Two ledgers:
//
//   - The user's lifetime totals get the turn's tokens and chat cost
//     (UsageAccumulator.Cost); rolled-up costs such as the query embedding
//     were added there by their own path already.
//   - The thread's Σ is the sum of its messages' costs, so the turn's full
//     cost (TurnCost) must sit on one of its messages. The assistant message
//     carries what was spent before it was written; anything later (the
//     thread title) is added onto it. A turn that never wrote an answer puts
//     its whole cost on the user message instead.
//
// settle books only what arrived since the previous call, so a turn can
// settle before its terminal event and again once a late call (the title of
// a failed turn) has finished, without counting anything twice.
type turnCostSettler struct {
	s             *server
	user          auth.User
	userMessageID string
	acc           *llm.UsageAccumulator
	// assistant is the persisted answer, nil until (unless) it is written.
	assistant *chat.Message

	mu sync.Mutex
	// rolledUsage and rolledCost are what the lifetime totals already have.
	rolledUsage llm.TokenUsage
	rolledCost  int64
	// booked is the target message and the total it carries now, which is
	// also the message_cost payload; ID is empty until something was booked.
	booked messageCost
}

// messageCost is the message_cost SSE payload: a message's settled cost, so
// the open thread's Σ is right without a reload.
type messageCost struct {
	ID          string `json:"id"`
	CostNanoUSD int64  `json:"costNanoUsd"`
}

func (c *turnCostSettler) setAssistant(m chat.Message) { c.assistant = &m }

// settle books what the turn spent since the last settle. Safe to call more
// than once and from the deferred exit path.
func (c *turnCostSettler) settle(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollUp(ctx)
	c.bookOnThread(ctx)
}

// settleAndReport settles and tells the client the message's new cost.
// Callers run it before the turn's terminal event ("done" or "error"): the
// client stops reading at either.
func (c *turnCostSettler) settleAndReport(ctx context.Context, stream *sse.Writer) {
	c.settle(ctx)
	c.mu.Lock()
	booked := c.booked
	c.mu.Unlock()
	if booked.ID != "" {
		_ = sendSSEJSON(stream, "message_cost", booked)
	}
}

func (c *turnCostSettler) rollUp(ctx context.Context) {
	total := c.acc.Total()
	cost, _ := c.acc.Cost()
	delta := usage.TokenDelta{
		PromptTokens:     total.PromptTokens - c.rolledUsage.PromptTokens,
		CompletionTokens: total.CompletionTokens - c.rolledUsage.CompletionTokens,
		CachedTokens:     total.PromptTokensDetails.CachedTokens - c.rolledUsage.PromptTokensDetails.CachedTokens,
		ReasoningTokens:  total.CompletionTokenDetails.ReasoningTokens - c.rolledUsage.CompletionTokenDetails.ReasoningTokens,
		TotalTokens:      total.TotalTokens - c.rolledUsage.TotalTokens,
		CostNanoUSD:      cost - c.rolledCost,
	}
	if delta.PromptTokens == 0 && delta.CompletionTokens == 0 && delta.TotalTokens == 0 && delta.CostNanoUSD == 0 {
		return
	}
	c.rolledUsage, c.rolledCost = total, cost
	c.s.recordUsage("tokens", func() error {
		return c.s.usage.AddTokens(ctx, c.user.ID, delta)
	})
}

func (c *turnCostSettler) bookOnThread(ctx context.Context) {
	full, priced := c.acc.TurnCost()
	if !priced || full <= 0 || c.s.thread == nil {
		return
	}
	if c.booked.ID == "" {
		// First booking: the target is the answer when there is one, which
		// already carries what was spent before it was written; otherwise the
		// user message, which carries no cost of its own.
		c.booked.ID = c.userMessageID
		if c.assistant != nil {
			c.booked.ID = c.assistant.ID
			if c.assistant.CostNanoUSD != nil {
				c.booked.CostNanoUSD = *c.assistant.CostNanoUSD
			}
		}
	}
	delta := full - c.booked.CostNanoUSD
	if delta <= 0 {
		return
	}
	if _, err := c.s.thread.AddMessageCost(ctx, c.user.ID, c.booked.ID, delta); err != nil {
		slog.Warn("book turn cost on thread failed", "message_id", c.booked.ID, "cost_nano_usd", delta, "err", err)
		return
	}
	c.booked.CostNanoUSD = full
}
