package turn

import (
	"strconv"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/schedule"
)

// The create tool names the prompt cap the service enforces, so the model
// writes a prompt that fits instead of learning the cap from a refusal.
func TestScheduleCreateToolNamesThePromptCap(t *testing.T) {
	prompt := scheduleCreateTool().Function.Parameters["properties"].(map[string]any)["prompt"].(map[string]any)
	if !strings.Contains(prompt["description"].(string), strconv.Itoa(schedule.MaxPromptLength)) {
		t.Fatalf("prompt description = %q, want the cap %d", prompt["description"], schedule.MaxPromptLength)
	}
	for _, tool := range []func() llm.Tool{scheduleCreateTool, scheduleUpdateTool} {
		if !strings.Contains(tool().Function.Description, "A run is short") {
			t.Fatalf("%s does not say a run is short", tool().Function.Name)
		}
	}
	if !strings.Contains(scheduleUpdateTool().Function.Description, "disable") {
		t.Fatal("schedule_update does not name disabling")
	}
}
