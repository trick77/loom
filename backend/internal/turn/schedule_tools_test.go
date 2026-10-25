package turn

import (
	"strconv"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
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

// schedule_run is offered with the other schedule tools and says the answer
// lands elsewhere, so the model does not also do the task in the chat.
func TestScheduleRunTool(t *testing.T) {
	var spec *toolSpec
	for i := range scheduleToolSpecs {
		if scheduleToolSpecs[i].name == scheduleRunToolName {
			spec = &scheduleToolSpecs[i]
		}
	}
	if spec == nil {
		t.Fatal("schedule_run is not registered")
	}
	tool := spec.schema()
	if tool.Function.Name != "schedule_run" || !strings.Contains(tool.Function.Description, "new thread") {
		t.Fatalf("tool = %+v", tool.Function)
	}
	if req, _ := tool.Function.Parameters["required"].([]any); len(req) != 1 || req[0] != "id" {
		t.Fatalf("required = %v", tool.Function.Parameters["required"])
	}
	if spec.offered(&Engine{}, chat.Thread{}, toolGate{}) {
		t.Fatal("offered without a schedule service")
	}
}
