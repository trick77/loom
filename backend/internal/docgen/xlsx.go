package docgen

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/trick77/spark/internal/artifact"
	"github.com/xuri/excelize/v2"
)

const maxXLSXRows = 5000

type XLSXGenerator struct{}

func (g XLSXGenerator) ToolName() string { return "create_xlsx_file" }

func (g XLSXGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name:        g.ToolName(),
		Description: "Create an XLSX spreadsheet from rows or CSV data.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"rows": map[string]any{
					"type":        "array",
					"description": "Spreadsheet rows, where each row is an array of cell values.",
					"items": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": []string{"string", "number", "boolean", "null"}},
					},
				},
				"csvData": map[string]any{"type": "string"},
			},
			"required":             []string{"filename"},
			"additionalProperties": false,
		},
	}
}

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

	book := excelize.NewFile()
	defer func() { _ = book.Close() }()
	for r, row := range rows {
		for c, value := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				return GeneratedMeta{}, err
			}
			if err := book.SetCellValue("Sheet1", cell, value); err != nil {
				return GeneratedMeta{}, err
			}
		}
	}
	_ = book.SetPanes("Sheet1", &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	if err := book.Write(w); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "xlsx", MIMEType: artifact.MIMEType("xlsx")}, nil
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
