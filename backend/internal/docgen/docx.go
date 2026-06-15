package docgen

import (
	"errors"
	"io"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
)

type DOCXGenerator struct{}

func (g DOCXGenerator) ToolName() string { return "create_docx_file" }

func (g DOCXGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name:        g.ToolName(),
		Description: "Create a DOCX document from Markdown or plain text content." + FileToolGuardrail,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"content":  map[string]any{"type": "string"},
			},
			"required":             []string{"filename", "content"},
			"additionalProperties": false,
		},
	}
}

func (g DOCXGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	content, ok := req.Payload["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return GeneratedMeta{}, errors.New("content is required")
	}
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/document.xml": docxDocumentXML(content),
	}
	if err := writeZipPackage(w, files); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "docx", MIMEType: artifact.MIMEType("docx")}, nil
}

func docxDocumentXML(content string) string {
	var body strings.Builder
	for _, line := range strings.Split(content, "\n") {
		text := strings.TrimSpace(line)
		if text == "" {
			body.WriteString(`<w:p/>`)
			continue
		}
		if strings.HasPrefix(text, "# ") {
			body.WriteString(`<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>`)
			body.WriteString(xmlText(strings.TrimSpace(strings.TrimPrefix(text, "# "))))
			body.WriteString(`</w:t></w:r></w:p>`)
			continue
		}
		body.WriteString(`<w:p><w:r><w:t>`)
		body.WriteString(xmlText(text))
		body.WriteString(`</w:t></w:r></w:p>`)
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>` + body.String() + `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body>
</w:document>`
}
