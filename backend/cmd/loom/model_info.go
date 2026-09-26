package main

import (
	"github.com/trick77/loom/internal/config"
	"github.com/trick77/loom/internal/llm"
)

// chatModelInfo is what /api/model reports: the chat model's facts when chat
// is on, nothing when it is off (a configured model without its key cannot
// answer, and the composer must not name it).
func chatModelInfo(cfg config.Config) llm.ModelInfo {
	if !cfg.ChatEnabled {
		return llm.ModelInfo{}
	}
	return cfg.ChatModels.Info()
}
