package httpapi

import (
	"encoding/json"
	"fmt"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
)

// Artifacts are embedded as JSON snapshots inside each message at save time — in
// the legacy Message.Artifacts array and in the artifact blocks of
// Message.ContentBlocks. A snapshot frozen then cannot know the artifact was
// later renamed or deleted, so we refresh those snapshots at read time with the
// artifact's current display filename and deleted status. This is what makes a
// rename propagate into the chat and a delete render a tombstone, without ever
// rewriting the persisted message rows.

// overlayMessageArtifacts rewrites every embedded artifact snapshot in the given
// messages using byID (which includes soft-deleted artifacts). An embedded id
// that is absent from byID is treated as deleted — the artifact was purged or is
// no longer accessible. Mutates messages in place.
func overlayMessageArtifacts(messages []chat.Message, byID map[string]artifact.Artifact) error {
	for i := range messages {
		updatedArtifacts, err := overlayArtifactArray(messages[i].Artifacts, byID)
		if err != nil {
			return fmt.Errorf("overlay message %s artifacts: %w", messages[i].ID, err)
		}
		messages[i].Artifacts = updatedArtifacts

		updatedBlocks, err := overlayContentBlocks(messages[i].ContentBlocks, byID)
		if err != nil {
			return fmt.Errorf("overlay message %s content blocks: %w", messages[i].ID, err)
		}
		messages[i].ContentBlocks = updatedBlocks
	}
	return nil
}

// collectArtifactIDs gathers every artifact id referenced by the messages, from
// both the Artifacts array and the artifact blocks of ContentBlocks. Duplicates
// are removed; order is not significant.
func collectArtifactIDs(messages []chat.Message) ([]string, error) {
	seen := make(map[string]struct{})
	var ids []string
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, message := range messages {
		objs, err := decodeArtifactObjects(message.Artifacts)
		if err != nil {
			return nil, fmt.Errorf("decode message %s artifacts: %w", message.ID, err)
		}
		for _, obj := range objs {
			add(artifactObjectID(obj))
		}
		blockArtifacts, err := decodeContentBlockArtifacts(message.ContentBlocks)
		if err != nil {
			return nil, fmt.Errorf("decode message %s content blocks: %w", message.ID, err)
		}
		for _, obj := range blockArtifacts {
			add(artifactObjectID(obj))
		}
	}
	return ids, nil
}

func overlayArtifactArray(raw json.RawMessage, byID map[string]artifact.Artifact) (json.RawMessage, error) {
	if isEmptyJSON(raw) {
		return raw, nil
	}
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, err
	}
	for _, obj := range objs {
		applyArtifactOverlay(obj, byID)
	}
	return json.Marshal(objs)
}

func overlayContentBlocks(raw json.RawMessage, byID map[string]artifact.Artifact) (json.RawMessage, error) {
	if isEmptyJSON(raw) {
		return raw, nil
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	for _, block := range blocks {
		if blockType(block) != "artifact" {
			continue
		}
		artifactRaw, ok := block["artifact"]
		if !ok {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(artifactRaw, &obj); err != nil {
			return nil, err
		}
		applyArtifactOverlay(obj, byID)
		encoded, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		block["artifact"] = encoded
	}
	return json.Marshal(blocks)
}

// applyArtifactOverlay refreshes one embedded artifact object in place: it sets
// displayFilename to the current name (when known) and a deleted flag. An id that
// is not in byID is flagged deleted and keeps its last-known snapshot name.
func applyArtifactOverlay(obj map[string]json.RawMessage, byID map[string]artifact.Artifact) {
	id := artifactObjectID(obj)
	if id == "" {
		return
	}
	current, ok := byID[id]
	if !ok {
		obj["deleted"] = jsonTrue
		return
	}
	if encoded, err := json.Marshal(current.DisplayFilename); err == nil {
		obj["displayFilename"] = encoded
	}
	if current.Deleted {
		obj["deleted"] = jsonTrue
	} else {
		obj["deleted"] = jsonFalse
	}
}

var (
	jsonTrue  = json.RawMessage("true")
	jsonFalse = json.RawMessage("false")
)

func decodeArtifactObjects(raw json.RawMessage) ([]map[string]json.RawMessage, error) {
	if isEmptyJSON(raw) {
		return nil, nil
	}
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, err
	}
	return objs, nil
}

func decodeContentBlockArtifacts(raw json.RawMessage) ([]map[string]json.RawMessage, error) {
	if isEmptyJSON(raw) {
		return nil, nil
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	var objs []map[string]json.RawMessage
	for _, block := range blocks {
		if blockType(block) != "artifact" {
			continue
		}
		artifactRaw, ok := block["artifact"]
		if !ok {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(artifactRaw, &obj); err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

func artifactObjectID(obj map[string]json.RawMessage) string {
	raw, ok := obj["id"]
	if !ok {
		return ""
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return ""
	}
	return id
}

func blockType(block map[string]json.RawMessage) string {
	raw, ok := block["type"]
	if !ok {
		return ""
	}
	var blockType string
	if err := json.Unmarshal(raw, &blockType); err != nil {
		return ""
	}
	return blockType
}

// isEmptyJSON reports whether a raw JSON field carries no array content worth
// walking: nil, empty, the literal null, or an empty array.
func isEmptyJSON(raw json.RawMessage) bool {
	switch string(raw) {
	case "", "null", "[]":
		return true
	default:
		return false
	}
}
