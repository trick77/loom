package imagegen

// toolDescription is the image tool's description for the model.
const toolDescription = "Generate a PNG or JPEG image from a text prompt. Use this when the user asks to create, draw, render, or generate an image. Generate at most one image per turn."

// toolProperties are the arguments the model fills. Built per call: callers
// add to the map.
func toolProperties() map[string]any {
	return map[string]any{
		"prompt": map[string]any{
			"type":        "string",
			"description": "Detailed visual prompt for the generated image. Maximum 4000 characters.",
		},
		"filename": map[string]any{
			"type":        "string",
			"description": "Optional output filename without path. The extension is added automatically.",
		},
		"aspect_ratio": map[string]any{
			"type":        "string",
			"enum":        AspectRatioNames(),
			"description": "Shape of the image, chosen from what the request depicts: 16:9 or 3:2 for wide scenes, landscapes, banners and desktop wallpapers; 9:16 or 2:3 for tall subjects, posters, book covers and phone wallpapers; 4:3 or 3:4 for a mild landscape or portrait lean. Omit this parameter entirely when the request implies no particular shape, and when editing an image that should keep its own proportions — a square is what you get by default, so it never needs to be asked for. Judge this from what the user actually asks for in whatever language they write in, not from specific words.",
		},
		"width": map[string]any{
			"type":        "integer",
			"description": "Output width in pixels. Only set this when the user asks for an exact size; otherwise use aspect_ratio and leave this unset. Rounded up to a multiple of 16, fitted to 256-2560 pixels per side, and width × height must not exceed 4,000,000 pixels (about 4 MP).",
		},
		"height": map[string]any{
			"type":        "integer",
			"description": "Output height in pixels. Only set this when the user asks for an exact size; otherwise use aspect_ratio and leave this unset. Rounded up to a multiple of 16, fitted to 256-2560 pixels per side, and width × height must not exceed 4,000,000 pixels (about 4 MP).",
		},
		"seed": map[string]any{
			"type":        "integer",
			"description": "Optional seed for reproducible generation.",
		},
		"output_format": map[string]any{
			"type":        "string",
			"enum":        []string{"png", "jpeg"},
			"description": "Output image format. Defaults to png.",
		},
		"safety_tolerance": map[string]any{
			"type":        "integer",
			"description": "Content-safety tolerance from 0 (strictest) to 5 (most permissive). Defaults to 2.",
		},
	}
}
