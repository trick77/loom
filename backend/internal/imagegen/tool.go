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
			"description": "From what the request depicts, in any language: 16:9/3:2 wide scenes, banners, desktop wallpapers; 9:16/2:3 posters, book covers, phone wallpapers; 4:3/3:4 a mild lean. Omit for no particular shape (square) or to keep an edited image's proportions.",
		},
		// width and height: the server rounds to multiples of 16 and fits
		// 256-2560 px a side, at most 4 MP; the model needs only when to set them.
		"width": map[string]any{
			"type":        "integer",
			"description": "Pixels, only for an exact size the user asks for (256-2560, at most 4 MP with height).",
		},
		"height": map[string]any{
			"type":        "integer",
			"description": "Pixels, only for an exact size the user asks for.",
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
