package web

import "mime"

// Go's mime package resolves .webmanifest from the OS mime.types database, which
// is not guaranteed to exist on the minimal (distroless) runtime image — without
// it http.FileServer serves the PWA manifest with a sniffed/empty type and the
// app is not installable. Register the mapping explicitly so it never depends on
// the base image shipping /etc/mime.types.
func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}
