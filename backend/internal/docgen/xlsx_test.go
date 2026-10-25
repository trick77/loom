package docgen

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestXLSXGeneratorWritesRows(t *testing.T) {
	gen := XLSXGenerator{}
	var out bytes.Buffer
	meta, err := gen.Generate(GenerateRequest{
		Filename: "sales.xlsx",
		Payload: map[string]any{
			"rows": []any{
				[]any{"Region", "Revenue", "Active", "Note"},
				[]any{"CH", float64(42), true, nil},
				[]any{" <R&D> ", 1.5, false, map[string]any{"a": "b"}},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "xlsx" || meta.MIMEType != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || out.Len() == 0 {
		t.Fatalf("meta = %#v len = %d", meta, out.Len())
	}

	assertWellFormedPackage(t, out.Bytes())
	assertConsistentPackage(t, out.Bytes())
	sheet := zipEntry(t, out.Bytes(), "xl/worksheets/sheet1.xml")
	for _, want := range []string{
		`<dimension ref="A1:D3"/>`,
		`<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>`,
		`<c r="A1" t="inlineStr"><is><t xml:space="preserve">Region</t></is></c>`,
		`<c r="A2" t="inlineStr"><is><t xml:space="preserve">CH</t></is></c>`,
		`<c r="B2"><v>42</v></c>`,
		`<c r="C2" t="b"><v>1</v></c>`,
		`<c r="A3" t="inlineStr"><is><t xml:space="preserve"> &lt;R&amp;D&gt; </t></is></c>`,
		`<c r="B3"><v>1.5</v></c>`,
		`<c r="C3" t="b"><v>0</v></c>`,
		`<c r="D3" t="inlineStr"><is><t xml:space="preserve">map[a:b]</t></is></c>`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("sheet1.xml lacks %s:\n%s", want, sheet)
		}
	}
	if strings.Contains(sheet, `r="D2"`) {
		t.Fatalf("null cell written:\n%s", sheet)
	}
}

func TestXLSXGeneratorTypesCSVCells(t *testing.T) {
	gen := XLSXGenerator{}
	var out bytes.Buffer
	_, err := gen.Generate(GenerateRequest{
		Filename: "csv.xlsx",
		Payload:  map[string]any{"csvData": "name,value\nalpha, 7 \nbeta,NaN\ngamma,inf\ndelta, -Infinity\n"},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	sheet := zipEntry(t, out.Bytes(), "xl/worksheets/sheet1.xml")
	for _, want := range []string{
		`<c r="B2"><v>7</v></c>`,
		`<c r="B3" t="inlineStr"><is><t xml:space="preserve">NaN</t></is></c>`,
		`<c r="B4" t="inlineStr"><is><t xml:space="preserve">inf</t></is></c>`,
		`<c r="B5" t="inlineStr"><is><t xml:space="preserve"> -Infinity</t></is></c>`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("sheet1.xml lacks %s:\n%s", want, sheet)
		}
	}
}

func TestXLSXGeneratorRejectsEmptyRows(t *testing.T) {
	gen := XLSXGenerator{}
	_, err := gen.Generate(GenerateRequest{
		Filename: "empty.xlsx",
		Payload:  map[string]any{"rows": []any{}},
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Generate() succeeded, want error")
	}
}

func TestXLSXGeneratorRejectsTooManyColumns(t *testing.T) {
	gen := XLSXGenerator{}
	_, err := gen.Generate(GenerateRequest{
		Filename: "wide.xlsx",
		Payload:  map[string]any{"rows": []any{make([]any, maxXLSXColumns+1)}},
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Generate() succeeded, want error")
	}
}

func TestXLSXCellName(t *testing.T) {
	cases := map[[2]int]string{
		{1, 1}: "A1", {26, 2}: "Z2", {27, 3}: "AA3", {52, 1}: "AZ1",
		{702, 1}: "ZZ1", {703, 1}: "AAA1", {maxXLSXColumns, 5000}: "XFD5000",
	}
	for in, want := range cases {
		if got := xlsxCellName(in[0], in[1]); got != want {
			t.Fatalf("xlsxCellName(%d, %d) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestTruncateUTF16(t *testing.T) {
	// The emoji is two UTF-16 units: it fits whole or not at all.
	if got := truncateUTF16("ab😀c", 3); got != "ab" {
		t.Fatalf("truncateUTF16 = %q", got)
	}
	if got := truncateUTF16("ab😀c", 4); got != "ab😀" {
		t.Fatalf("truncateUTF16 = %q", got)
	}
	if got := truncateUTF16("abc", 3); got != "abc" {
		t.Fatalf("truncateUTF16 = %q", got)
	}
}

// Gated sample for checking the package with a real spreadsheet reader.
// Skipped unless OUT_DIR is set, e.g.:
//
//	OUT_DIR=/tmp go test ./internal/docgen -run TestGenerateVerifyXLSX
func TestGenerateVerifyXLSX(t *testing.T) {
	outDir := os.Getenv("OUT_DIR")
	if outDir == "" {
		t.Skip("set OUT_DIR to write the sample")
	}
	header := []any{"Name", "Amount", "Flag", "Empty", "Text"}
	for len(header) < 30 {
		header = append(header, "Col"+xlsxCellName(len(header)+1, 1))
	}
	wide := make([]any, 30)
	wide[29] = "AD3"
	var out bytes.Buffer
	_, err := XLSXGenerator{}.Generate(GenerateRequest{
		Filename: "verify.xlsx",
		Payload: map[string]any{"rows": []any{
			header,
			[]any{" padded ", float64(42), true, nil, "line1\nline2 <&\"'>"},
			wide,
			[]any{"Zürich ✓ 😀", -1.25e-7, false, nil, 1e21},
		}},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "verify.xlsx"), out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestXLSXDimensionSingleCell(t *testing.T) {
	sheet, err := xlsxSheetXML([][]any{{"only"}})
	if err != nil {
		t.Fatalf("xlsxSheetXML() error = %v", err)
	}
	if !strings.Contains(sheet, `<dimension ref="A1"/>`) {
		t.Fatalf("sheet lacks single-cell dimension:\n%s", sheet)
	}
}

// assertConsistentPackage checks what a spreadsheet reader resolves: every
// part has a content type, every override and relationship target exists,
// and the worksheet children keep the schema's sequence.
func assertConsistentPackage(t *testing.T, data []byte) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	parts := map[string]bool{}
	for _, file := range reader.File {
		parts[file.Name] = true
	}

	var types struct {
		Defaults []struct {
			Extension string `xml:"Extension,attr"`
		} `xml:"Default"`
		Overrides []struct {
			PartName string `xml:"PartName,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal([]byte(zipEntry(t, data, "[Content_Types].xml")), &types); err != nil {
		t.Fatalf("[Content_Types].xml: %v", err)
	}
	typed := map[string]bool{}
	for _, override := range types.Overrides {
		name := strings.TrimPrefix(override.PartName, "/")
		if !parts[name] {
			t.Fatalf("content type override for missing part %s", override.PartName)
		}
		typed[name] = true
	}
	extensions := map[string]bool{}
	for _, def := range types.Defaults {
		extensions[def.Extension] = true
	}
	for name := range parts {
		if name != "[Content_Types].xml" && !typed[name] && !extensions[strings.TrimPrefix(path.Ext(name), ".")] {
			t.Fatalf("part %s has no content type", name)
		}
	}

	for name := range parts {
		if !strings.HasSuffix(name, ".rels") {
			continue
		}
		var rels struct {
			Relationships []struct {
				Target string `xml:"Target,attr"`
			} `xml:"Relationship"`
		}
		if err := xml.Unmarshal([]byte(zipEntry(t, data, name)), &rels); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// "xl/_rels/workbook.xml.rels" resolves relative to "xl/".
		base := path.Dir(path.Dir(name))
		for _, rel := range rels.Relationships {
			if target := path.Join(base, rel.Target); !parts[target] {
				t.Fatalf("%s targets missing part %s", name, target)
			}
		}
	}

	decoder := xml.NewDecoder(strings.NewReader(zipEntry(t, data, "xl/worksheets/sheet1.xml")))
	var children []string
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("sheet1.xml: %v", err)
		}
		switch el := token.(type) {
		case xml.StartElement:
			if depth == 1 {
				children = append(children, el.Name.Local)
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	if got := strings.Join(children, ","); got != "dimension,sheetViews,sheetData" {
		t.Fatalf("worksheet children = %s", got)
	}
}

func assertWellFormedPackage(t *testing.T, data []byte) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewReader() error = %v", err)
	}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("Open(%s) error = %v", file.Name, err)
		}
		decoder := xml.NewDecoder(rc)
		for {
			_, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("%s is not well-formed: %v", file.Name, err)
			}
		}
		_ = rc.Close()
	}
}
