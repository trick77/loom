package main

import (
	"os"
	"strings"
	"testing"
)

func TestComposePassesBFLImageGenerationEnv(t *testing.T) {
	for _, path := range []string{
		"../../../compose.yaml",
		"../../../compose.dev.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			compose := string(data)

			for _, want := range []string{
				`SLOPR_BFL_BASE_URL: "${SLOPR_BFL_BASE_URL:-https://api.bfl.ai/v1}"`,
				`SLOPR_BFL_API_KEY: "${SLOPR_BFL_API_KEY:-}"`,
				`SLOPR_BFL_MODEL: "${SLOPR_BFL_MODEL:-flux-2-klein-4b}"`,
			} {
				if !strings.Contains(compose, want) {
					t.Fatalf("%s does not pass %s into the slopr container", path, strings.Split(want, ":")[0])
				}
			}
		})
	}
}
