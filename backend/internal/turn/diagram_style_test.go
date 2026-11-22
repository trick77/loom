package turn

import (
	"strings"
	"testing"
)

// The browser draws a fence only when it opens with ```mermaid, and only the
// types the table names are asked for: a type missing here is one the model
// improvises, untested by the UI's corpus.
func TestDiagramStyleNamesFenceAndTypes(t *testing.T) {
	if !strings.Contains(diagramStyle, "```mermaid") {
		t.Error("diagramStyle does not say how to open the fence")
	}
	for _, kind := range []string{"mindmap", "flowchart TD", "sequenceDiagram", "stateDiagram-v2", "erDiagram", "timeline", "quadrantChart"} {
		if !strings.Contains(diagramStyle, kind) {
			t.Errorf("diagramStyle's type table lacks %s", kind)
		}
	}
	// The eval's failures, each a rule: numbers went into an xychart fence;
	// an entity named CLASS (a keyword), a mark after a space and a mark on an
	// edge label made the parser refuse the picture; a mind map with six
	// children a branch was too wide for the package.
	for _, rule := range []string{"xychart-beta", "CLASS", "no space", "edge label", "classDef", "<br/>", "never more than 4"} {
		if !strings.Contains(diagramStyle, rule) {
			t.Errorf("diagramStyle lacks the rule naming %q", rule)
		}
	}
	// A structure drawn by the image model garbles its labels and counts as an
	// action, which a turn that read outside content may not take.
	if !strings.Contains(diagramStyle, "image generation tool") {
		t.Error("diagramStyle does not keep diagrams away from the image tool")
	}
}
