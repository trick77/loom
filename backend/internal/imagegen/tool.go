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
			"type": "string",
			// No enum: a ratio the list lacked (3:1) sent the model to width
			// and height, at four times the pixels.
			"description": "W:H, any ratio (past 4:1 drawn at 4:1); one the user names goes in as named (3:1, 21:9). Else from what the request depicts, in any language: 16:9/3:2 wide scenes, banners, desktop wallpapers; 9:16/2:3 posters, book covers, phone wallpapers; 4:3/3:4 a mild lean. Omit for no particular shape (square) or to keep an edited image's proportions.",
		},
		// width and height: the server rounds to multiples of 16 and fits
		// 256-2560 px a side, at most 4 MP; the model needs only when to set them.
		"width": map[string]any{
			"type":        "integer",
			"description": "Only when the user names a size in pixels (256-2560, at most 4 MP with height); a shape goes in aspect_ratio.",
		},
		"height": map[string]any{
			"type":        "integer",
			"description": "Only when the user names a size in pixels.",
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
