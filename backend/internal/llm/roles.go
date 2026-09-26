package llm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/trick77/llmwire"
)

// Roles names the llmwire model for each kind of call loom makes. Which model
// fills a role is configuration; what a model can do is its llmwire profile.
type Roles struct {
	// Chat answers turns, writes the forced final answer and the prose
	// helpers (project memory and description). Needs tool calling.
	Chat string
	// Gate runs the short calls a turn waits on: titles, classification,
	// image intent. Defaults to Chat.
	Gate string
	// Vision answers turns and describes images when a message carries one.
	// Needs image input. Defaults to Chat.
	Vision string
}

// withDefaults fills Gate and Vision from Chat.
func (r Roles) withDefaults() Roles {
	if r.Gate == "" {
		r.Gate = r.Chat
	}
	if r.Vision == "" {
		r.Vision = r.Chat
	}
	return r
}

// ids lists the distinct models the roles name, for the one llmwire client
// that serves them all.
func (r Roles) ids() []string {
	var out []string
	for _, id := range []string{r.Chat, r.Gate, r.Vision} {
		if id != "" && !containsString(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// Resolved is a set of roles checked against the registry.
type Resolved struct {
	Roles  Roles
	chat   *llmwire.Profile
	gate   *llmwire.Profile
	vision *llmwire.Profile
}

// ResolveRoles checks every role's model against what the role needs and
// fills the defaulted ones. A missing chat model, an unknown id or a model
// short of its role's needs is an error naming the models that would work.
func ResolveRoles(reg *llmwire.Registry, roles Roles) (Resolved, error) {
	if reg == nil {
		reg = llmwire.Default()
	}
	roles = roles.withDefaults()
	chatNeeds := llmwire.Needs{Tools: true, Streaming: true}
	if roles.Chat == "" {
		return Resolved{}, fmt.Errorf("llm: no chat model configured; valid choices are %s",
			strings.Join(reg.ChatModels(chatNeeds), ", "))
	}
	out := Resolved{Roles: roles}
	var err error
	if out.chat, err = require(reg, "chat", roles.Chat, chatNeeds); err != nil {
		return Resolved{}, err
	}
	if out.gate, err = require(reg, "gate", roles.Gate, llmwire.Needs{}); err != nil {
		return Resolved{}, err
	}
	if out.vision, err = require(reg, "vision", roles.Vision, llmwire.Needs{Vision: true, Streaming: true}); err != nil {
		return Resolved{}, err
	}
	return out, nil
}

func require(reg *llmwire.Registry, role, id string, needs llmwire.Needs) (*llmwire.Profile, error) {
	p, err := reg.Require(id, needs)
	if err == nil {
		return p, nil
	}
	var unknown *llmwire.UnknownModelError
	if errors.As(err, &unknown) {
		// llmwire's unknown-model error names no alternatives; the operator
		// fixing a typo needs them.
		return nil, fmt.Errorf("llm: %s model: %w; valid choices are %s",
			role, err, strings.Join(reg.ChatModels(needs), ", "))
	}
	return nil, fmt.Errorf("llm: %s model: %w", role, err)
}

// KeyEnvs lists the key variables the roles' providers read, each once.
func (r Resolved) KeyEnvs() []string {
	var out []string
	for _, p := range []*llmwire.Profile{r.chat, r.gate, r.vision} {
		if p == nil {
			continue
		}
		if env := p.APIKeyEnv(); env != "" && !containsString(out, env) {
			out = append(out, env)
		}
	}
	return out
}

// ModelInfo is what the UI and the startup line show about the chat model.
type ModelInfo struct {
	ID            string `json:"model"`
	DisplayName   string `json:"displayName"`
	ContextWindow int64  `json:"contextWindow"`
}

// Info describes the chat model from its profile.
func (r Resolved) Info() ModelInfo {
	if r.chat == nil {
		return ModelInfo{}
	}
	return ModelInfo{ID: r.chat.ID, DisplayName: r.chat.DisplayName, ContextWindow: r.chat.Limits.Context}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
