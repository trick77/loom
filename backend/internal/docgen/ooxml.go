package docgen

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"sort"
)

func writeZipPackage(w io.Writer, files map[string]string) error {
	zw := zip.NewWriter(w)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := io.WriteString(entry, files[name]); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

func xmlText(value string) string {
	var out bytes.Buffer
	if err := xml.EscapeText(&out, []byte(value)); err != nil {
		return ""
	}
	return out.String()
}
