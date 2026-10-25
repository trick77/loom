package docgen

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/trick77/loom/internal/artifact"
)

const (
	maxXLSXRows = 5000
	// Excel's sheet limits: 16384 columns (XFD), 32767 UTF-16 units per cell.
	maxXLSXColumns   = 16384
	maxXLSXCellChars = 32767
)

// XLSXGenerator generates XLSX spreadsheets.
type XLSXGenerator struct{}

// ToolName returns the name of the XLSX file creation tool.
func (g XLSXGenerator) ToolName() string { return "create_xlsx_file" }

// Schema returns the JSON schema for the XLSX generation tool.
func (g XLSXGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name: g.ToolName(),
		Description: "Create an XLSX spreadsheet. Provide exactly one of `rows` or `csvData` (if both " +
			"are given, `rows` is used). The first row is treated as the header and frozen. Max 5000 rows." + FileToolGuardrail,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string", "description": "Short, descriptive output filename based on the spreadsheet's content, without a path or file extension (e.g. `sales-data`)."},
				"rows": map[string]any{
					"type":        "array",
					"description": "Spreadsheet rows, where each row is an array of cell values. First row is the header. Ignored if omitted; provide this or `csvData`.",
					"items": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": []string{"string", "number", "boolean", "null"}},
					},
				},
				"csvData": map[string]any{"type": "string", "description": "Alternative to `rows`: raw CSV text (first row is the header). Ignored if `rows` is provided."},
			},
			"required":             []string{"filename"},
			"additionalProperties": false,
		},
	}
}

// Generate creates an XLSX spreadsheet from the request payload and writes it to w.
func (g XLSXGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	rows, err := spreadsheetRows(req.Payload)
	if err != nil {
		return GeneratedMeta{}, err
	}
	if len(rows) == 0 {
		return GeneratedMeta{}, errors.New("rows or csvData is required")
	}
	if len(rows) > maxXLSXRows {
		return GeneratedMeta{}, fmt.Errorf("too many rows")
	}

	sheet, err := xlsxSheetXML(rows)
	if err != nil {
		return GeneratedMeta{}, err
	}
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`,
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": sheet,
	}
	if err := writeZipPackage(w, files); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "xlsx", MIMEType: artifact.MIMEType("xlsx")}, nil
}

// xlsxSheetXML renders the worksheet with the header row frozen. sheetViews
// must precede sheetData (the schema is a strict sequence).
func xlsxSheetXML(rows [][]any) (string, error) {
	var data strings.Builder
	for r, row := range rows {
		if len(row) > maxXLSXColumns {
			return "", fmt.Errorf("too many columns in row %d", r+1)
		}
		fmt.Fprintf(&data, `<row r="%d">`, r+1)
		for c, value := range row {
			data.WriteString(xlsxCell(xlsxCellName(c+1, r+1), value))
		}
		data.WriteString(`</row>`)
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/><selection pane="bottomLeft" activeCell="A2" sqref="A2"/></sheetView></sheetViews>
  <sheetData>` + data.String() + `</sheetData>
</worksheet>`, nil
}

// xlsxCell renders one cell: numbers and booleans typed, everything else an
// inline string. A null is no cell at all.
func xlsxCell(ref string, value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return `<c r="` + ref + `"><v>` + strconv.FormatFloat(v, 'g', -1, 64) + `</v></c>`
		}
	case bool:
		if v {
			return `<c r="` + ref + `" t="b"><v>1</v></c>`
		}
		return `<c r="` + ref + `" t="b"><v>0</v></c>`
	case string:
		return xlsxStringCell(ref, v)
	}
	return xlsxStringCell(ref, fmt.Sprint(value))
}

func xlsxStringCell(ref, value string) string {
	return `<c r="` + ref + `" t="inlineStr"><is><t xml:space="preserve">` + xmlText(truncateUTF16(value, maxXLSXCellChars)) + `</t></is></c>`
}

// xlsxCellName converts 1-based coordinates to an A1 reference (27 → AA).
func xlsxCellName(col, row int) string {
	var name []byte
	for ; col > 0; col = (col - 1) / 26 {
		name = append([]byte{byte('A' + (col-1)%26)}, name...)
	}
	return string(name) + strconv.Itoa(row)
}

func truncateUTF16(value string, limit int) string {
	units := 0
	for i, r := range value {
		n := utf16.RuneLen(r)
		if units+n > limit {
			return value[:i]
		}
		units += n
	}
	return value
}

func spreadsheetRows(payload map[string]any) ([][]any, error) {
	if raw, ok := payload["rows"].([]any); ok {
		rows := make([][]any, 0, len(raw))
		for _, item := range raw {
			row, ok := item.([]any)
			if !ok {
				return nil, errors.New("rows must contain arrays")
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
	raw, ok := payload["csvData"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	reader := csv.NewReader(strings.NewReader(raw))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	rows := make([][]any, 0, len(records))
	for _, record := range records {
		row := make([]any, 0, len(record))
		for _, value := range record {
			row = append(row, spreadsheetCellValue(value))
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func spreadsheetCellValue(value string) any {
	trimmed := strings.TrimSpace(value)
	if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return parsed
	}
	return value
}
