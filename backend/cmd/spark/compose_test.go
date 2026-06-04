package main

import (
	"os"
	"strings"
	"testing"
)

func TestComposePassesBFLImageGenerationEnv(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)

	for _, want := range []string{
		`SPARK_BFL_BASE_URL: "${SPARK_BFL_BASE_URL:-https://api.bfl.ai/v1}"`,
		`SPARK_BFL_API_KEY: "${SPARK_BFL_API_KEY:-}"`,
		`SPARK_BFL_MODEL: "${SPARK_BFL_MODEL:-flux-2-klein-4b}"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml does not pass %s into the spark container", strings.Split(want, ":")[0])
		}
	}
}
