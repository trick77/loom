package mcp

import "strings"

// The image tool's contract (docs/imagegen.md) as loom addresses it. The
// loom-imagegen sidecar serves it from package imagegen; its tests hold these
// to that package's names.
const (
	ImagegenServer         = "imagegen"
	ImagegenTool           = "generate"
	ImagegenArgInputImages = "input_images"
	ImagegenArgTextHeavy   = "text_heavy"
	ImagegenArgModel       = "model"
	// ImagegenMaxInputBytes is the largest input image the contract takes.
	ImagegenMaxInputBytes = 20 << 20
	// ImagegenMaxPromptRunes is the longest prompt the contract takes.
	ImagegenMaxPromptRunes = 4000
)

// ImagegenServerConfig is the loom-imagegen sidecar's endpoint: loom's own
// server, allowed to make artifacts. Best-effort like tunewire: a dead
// sidecar costs the image tool, never boot.
func ImagegenServerConfig(baseURL, token string) ServerConfig {
	return ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       strings.TrimRight(baseURL, "/") + "/mcp",
		Headers:   map[string]string{"Authorization": "Bearer " + token},
		Artifacts: true,
		BuiltIn:   true,
	}
}
