package sidecar

// UserAgent is how loom and its sidecars name themselves to outside services:
// the program, its release and where to find it ("loom-shop/0.12.0
// (+https://github.com/trick77/loom)"). Map providers ask for it, and a shop's
// image host refused a name without it.
func UserAgent(program, version string) string {
	return program + "/" + version + " (+https://github.com/trick77/loom)"
}
