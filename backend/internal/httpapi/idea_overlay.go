package httpapi

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/ideas"
	"github.com/trick77/loom/internal/turn"
)

// ideaKey is the JSON key only an idea card block carries: a column without
// it is skipped undecoded.
var ideaKey = []byte(`"idea"`)

// An idea card is stored in its message as it was proposed. What the user
// did with it since (saved, deleted, done, a proposal dismissed or purged)
// is overlaid on every load, like an artifact's rename, without rewriting
// the stored row. One user-scoped query for the whole thread; a column that
// does not decode is left as it is.
func (s *server) refreshMessageIdeas(ctx context.Context, userID string, messages []chat.Message) error {
	if s.ideas == nil {
		return nil
	}
	decoded := make(map[int][]map[string]json.RawMessage)
	seen := map[string]bool{}
	var ids []string
	for i, m := range messages {
		if chat.IsEmptyJSON(m.ContentBlocks) || !bytes.Contains(m.ContentBlocks, ideaKey) {
			continue
		}
		var blocks []map[string]json.RawMessage
		if json.Unmarshal(m.ContentBlocks, &blocks) != nil {
			continue
		}
		decoded[i] = blocks
		for _, block := range blocks {
			if card, ok := ideaCardOfBlock(block); ok && !seen[card.IdeaID] {
				seen[card.IdeaID] = true
				ids = append(ids, card.IdeaID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	byID, err := s.ideas.GetMany(ctx, userID, ids)
	if err != nil {
		return err
	}
	for i, blocks := range decoded {
		for _, block := range blocks {
			card, ok := ideaCardOfBlock(block)
			if !ok {
				continue
			}
			next := &turn.IdeaCard{IdeaID: card.IdeaID, Action: card.Action, State: ideaCardGone, Title: card.Title, Reason: card.Reason, DueOn: card.DueOn, RemindOn: card.RemindOn}
			// A proposal past its retention can no longer be confirmed: gone,
			// whether or not a purge has run yet.
			idea, found := byID[card.IdeaID]
			expired := found && idea.State == ideas.StateProposed && s.clock().Sub(idea.CreatedAt) > proposalRetention
			if found && !expired {
				next = ideaCardOf(idea, card.Action)
			}
			encoded, err := json.Marshal(next)
			if err != nil {
				return err
			}
			block["idea"] = encoded
		}
		raw, err := json.Marshal(blocks)
		if err != nil {
			return err
		}
		messages[i].ContentBlocks = raw
	}
	return nil
}

// ideaCardOfBlock reads an idea block's card.
func ideaCardOfBlock(block map[string]json.RawMessage) (turn.IdeaCard, bool) {
	var card turn.IdeaCard
	if blockType(block) != "idea" || json.Unmarshal(block["idea"], &card) != nil || card.IdeaID == "" {
		return turn.IdeaCard{}, false
	}
	return card, true
}
