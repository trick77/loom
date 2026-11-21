package httpapi

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/turn"
)

// actionKey is the JSON key only an action card block carries.
var actionKey = []byte(`"action"`)

// An action card is stored in its message as it was proposed. Whether the
// user ran or dismissed it since, and what the call returned, is overlaid on
// every load from tool_actions, like an idea card's state. One user-scoped
// query for the whole thread; a card whose action is no longer stored is
// gone, a column that does not decode is left as it is.
func (s *server) refreshMessageActions(ctx context.Context, userID string, messages []chat.Message) error {
	if s.actions == nil {
		return nil
	}
	decoded := make(map[int][]map[string]json.RawMessage)
	var ids []string
	for i, m := range messages {
		if chat.IsEmptyJSON(m.ContentBlocks) || !bytes.Contains(m.ContentBlocks, actionKey) {
			continue
		}
		var blocks []map[string]json.RawMessage
		if json.Unmarshal(m.ContentBlocks, &blocks) != nil {
			continue
		}
		decoded[i] = blocks
		for _, block := range blocks {
			if card, ok := actionCardOfBlock(block); ok {
				ids = append(ids, card.ActionID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	byID, err := s.actions.GetMany(ctx, userID, ids)
	if err != nil {
		return err
	}
	for i, blocks := range decoded {
		for _, block := range blocks {
			card, ok := actionCardOfBlock(block)
			if !ok {
				continue
			}
			if action, found := byID[card.ActionID]; found {
				card = withAction(card, action)
			} else {
				card.State, card.Result = turn.ActionCardGone, nil
			}
			encoded, err := json.Marshal(card)
			if err != nil {
				return err
			}
			block["action"] = encoded
		}
		raw, err := json.Marshal(blocks)
		if err != nil {
			return err
		}
		messages[i].ContentBlocks = raw
	}
	return nil
}

// actionCardOfBlock reads an action block's card.
func actionCardOfBlock(block map[string]json.RawMessage) (turn.ActionCard, bool) {
	var card turn.ActionCard
	if blockType(block) != "action" || json.Unmarshal(block["action"], &card) != nil || card.ActionID == "" {
		return turn.ActionCard{}, false
	}
	return card, true
}
