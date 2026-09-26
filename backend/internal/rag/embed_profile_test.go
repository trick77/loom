package rag

import "testing"

// The key variable is the profile provider's, never restated in loom.
func TestEmbedAPIKeyEnvComesFromTheProfile(t *testing.T) {
	if got := EmbedAPIKeyEnv(); got == "" || got != embedProfile.APIKeyEnv() {
		t.Fatalf("EmbedAPIKeyEnv() = %q, want the profile's %q", got, embedProfile.APIKeyEnv())
	}
}
