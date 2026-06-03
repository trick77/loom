package docgen

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestXLSXGeneratorWritesRows(t *testing.T) {
	gen := XLSXGenerator{}
	var out bytes.Buffer
	meta, err := gen.Generate(GenerateRequest{
		Filename: "sales.xlsx",
		Payload: map[string]any{
			"rows": []any{
				[]any{"Region", "Revenue"},
				[]any{"CH", float64(42)},
			},
		},
	}, &out)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if meta.Extension != "xlsx" || meta.MIMEType != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || out.Len() == 0 {
		t.Fatalf("meta = %#v len = %d", meta, out.Len())
	}

	book, err := excelize.OpenReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()
	value, err := book.GetCellValue("Sheet1", "A2")
	if err != nil {
		t.Fatalf("GetCellValue() error = %v", err)
	}
	if value != "CH" {
		t.Fatalf("A2 = %q", value)
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
