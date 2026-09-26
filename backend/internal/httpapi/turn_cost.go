package httpapi

import (
	"context"
	"log/slog"
	"sync"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/usage"
)

// turnCostSettler books everything a streamed turn spent, once, whichever way
// the turn ends. Two ledgers:
//
//   - The user's lifetime totals get the turn's tokens and chat cost
//     (UsageAccumulator.Cost); rolled-up costs such as the query embedding
//     were added there by their own path already.
//   - The thread's Σ is the sum of its messages' costs, so the turn's full
//     cost (TurnCost) must sit on one of its messages. The assistant message
//     carries what was spent before it was written; anything later (the
//     thread title) is added onto it. A turn that never wrote an answer puts
//     its whole cost on the user message instead.
type turnCostSettler struct {
	s             *server
	user          auth.User
	userMessageID string
	acc           *llm.UsageAccumulator
	// assistant is the persisted answer, nil until (unless) it is written.
	assistant *chat.Message
	once      sync.Once
}

func (c *turnCostSettler) setAssistant(m chat.Message) { c.assistant = &m }

// settle runs at most once. It must run after every call of the turn has
// finished, the thread title and the reasoning-title goroutines included.
func (c *turnCostSettler) settle(ctx context.Context) {
	c.once.Do(func() {
		c.rollUp(ctx)
		c.bookOnThread(ctx)
	})
}

func (c *turnCostSettler) rollUp(ctx context.Context) {
	turnUsage := c.acc.Total()
	turnCost, _ := c.acc.Cost()
	if !turnUsage.Present() && turnCost == 0 {
		return
	}
	c.s.recordUsage("tokens", func() error {
		return c.s.usage.AddTokens(ctx, c.user.ID, usage.TokenDelta{
			PromptTokens:     turnUsage.PromptTokens,
			CompletionTokens: turnUsage.CompletionTokens,
			CachedTokens:     turnUsage.PromptTokensDetails.CachedTokens,
			ReasoningTokens:  turnUsage.CompletionTokenDetails.ReasoningTokens,
			TotalTokens:      turnUsage.TotalTokens,
			CostNanoUSD:      turnCost,
		})
	})
}

func (c *turnCostSettler) bookOnThread(ctx context.Context) {
	full, priced := c.acc.TurnCost()
	if !priced || full <= 0 || c.s.thread == nil {
		return
	}
	messageID, delta := c.userMessageID, full
	if c.assistant != nil {
		messageID = c.assistant.ID
		if c.assistant.CostNanoUSD != nil {
			delta -= *c.assistant.CostNanoUSD
		}
	}
	if delta <= 0 {
		return
	}
	if _, err := c.s.thread.AddMessageCost(ctx, c.user.ID, messageID, delta); err != nil {
		slog.Warn("book turn cost on thread failed", "message_id", messageID, "cost_nano_usd", delta, "err", err)
	}
}
