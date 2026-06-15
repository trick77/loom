package docgen

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/trick77/slopr/internal/artifact"
)

type PPTXGenerator struct{}

type pptxSlide struct {
	Layout       string
	Title        string
	Subtitle     string
	Bullets      []string
	ColumnsLeft  []string
	ColumnsRight []string
	Number       string
	Caption      string
	Quote        string
	Attribution  string
	Table        [][]string
}

func (g PPTXGenerator) ToolName() string { return "create_pptx_presentation" }

func (g PPTXGenerator) Schema() ToolSchema {
	return ToolSchema{
		Name: g.ToolName(),
		Description: "Create a styled PPTX presentation. Open with a 'title' slide, use 'section' " +
			"dividers between parts, choose 'table' for tabular data, 'two-column' to compare, " +
			"'big-number' to highlight a stat, and 'quote' for a pull quote. Vary layouts; do not " +
			"put everything in plain bullets. Every slide needs a title (used as the heading), except " +
			"the 'quote' layout, which displays only the quote and attribution." + FileToolGuardrail,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename": map[string]any{"type": "string"},
				"title":    map[string]any{"type": "string"},
				"slides": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"layout": map[string]any{
								"type":        "string",
								"enum":        []string{"title", "section", "bullets", "two-column", "big-number", "quote", "table"},
								"description": "Slide layout. Defaults to bullets.",
							},
							"title":    map[string]any{"type": "string"},
							"subtitle": map[string]any{"type": "string"},
							"bullets":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"columns": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"left":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
									"right": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								},
							},
							"number":      map[string]any{"type": "string"},
							"caption":     map[string]any{"type": "string"},
							"quote":       map[string]any{"type": "string"},
							"attribution": map[string]any{"type": "string"},
							"table": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							},
						},
						"required": []string{"title"},
					},
				},
			},
			"required":             []string{"filename", "slides"},
			"additionalProperties": false,
		},
	}
}

func (g PPTXGenerator) Generate(req GenerateRequest, w io.Writer) (GeneratedMeta, error) {
	slides, err := presentationSlides(req.Payload)
	if err != nil {
		return GeneratedMeta{}, err
	}
	if len(slides) == 0 {
		return GeneratedMeta{}, errors.New("slides are required")
	}
	title, _ := req.Payload["title"].(string)
	if strings.TrimSpace(title) == "" {
		title = slides[0].Title
	}
	if err := writeZipPackage(w, pptxPackage(title, slides)); err != nil {
		return GeneratedMeta{}, err
	}
	return GeneratedMeta{DisplayFilename: req.Filename, Extension: "pptx", MIMEType: artifact.MIMEType("pptx")}, nil
}

func presentationSlides(payload map[string]any) ([]pptxSlide, error) {
	rawSlides, ok := payload["slides"].([]any)
	if !ok {
		return nil, errors.New("slides are required")
	}
	slides := make([]pptxSlide, 0, len(rawSlides))
	for _, raw := range rawSlides {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("slides must contain objects")
		}
		title, ok := item["title"].(string)
		if !ok || strings.TrimSpace(title) == "" {
			return nil, errors.New("slide title is required")
		}
		slide := pptxSlide{
			Layout:      strings.TrimSpace(stringField(item, "layout")),
			Title:       strings.TrimSpace(title),
			Subtitle:    strings.TrimSpace(stringField(item, "subtitle")),
			Number:      strings.TrimSpace(stringField(item, "number")),
			Caption:     strings.TrimSpace(stringField(item, "caption")),
			Quote:       strings.TrimSpace(stringField(item, "quote")),
			Attribution: strings.TrimSpace(stringField(item, "attribution")),
		}
		bullets, err := stringList(item["bullets"], "slide bullets")
		if err != nil {
			return nil, err
		}
		slide.Bullets = bullets
		if cols, ok := item["columns"].(map[string]any); ok {
			if slide.ColumnsLeft, err = stringList(cols["left"], "column"); err != nil {
				return nil, err
			}
			if slide.ColumnsRight, err = stringList(cols["right"], "column"); err != nil {
				return nil, err
			}
		}
		slide.Table = stringMatrix(item["table"])
		slides = append(slides, slide)
	}
	return slides, nil
}

func stringField(item map[string]any, key string) string {
	v, _ := item[key].(string)
	return v
}

// stringList converts a JSON array of strings, skipping empties. nil input → nil slice.
func stringList(raw any, label string) ([]string, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New(label + " must be strings")
		}
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out, nil
}

// stringMatrix converts a JSON array of string-arrays (table rows). Malformed cells are skipped.
func stringMatrix(raw any) [][]string {
	rows, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out [][]string
	for _, r := range rows {
		cells, ok := r.([]any)
		if !ok {
			continue
		}
		row := make([]string, 0, len(cells))
		for _, c := range cells {
			s, _ := c.(string)
			row = append(row, strings.TrimSpace(s))
		}
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	return out
}

func pptxPackage(title string, slides []pptxSlide) map[string]string {
	files := map[string]string{
		"[Content_Types].xml":               pptxContentTypes(len(slides)),
		"_rels/.rels":                       pptxRootRels(),
		"docProps/app.xml":                  pptxAppXML(len(slides)),
		"docProps/core.xml":                 pptxCoreXML(title),
		"ppt/presentation.xml":              pptxPresentationXML(len(slides)),
		"ppt/_rels/presentation.xml.rels":   pptxPresentationRels(len(slides)),
		"ppt/presProps.xml":                 `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:presentationPr xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/>`,
		"ppt/viewProps.xml":                 `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:viewPr xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/>`,
		"ppt/tableStyles.xml":               `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}"/>`,
		"ppt/theme/theme1.xml":              pptxThemeXML(),
		"ppt/slideMasters/slideMaster1.xml": pptxSlideMasterXML(),
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/>
</Relationships>`,
		"ppt/slideLayouts/slideLayout1.xml": pptxSlideLayoutXML(),
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>
</Relationships>`,
	}
	for i, slide := range slides {
		files[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)] = renderSlide(slide)
		files[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1)] = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
</Relationships>`
	}
	return files
}

func pptxContentTypes(slideCount int) string {
	var overrides strings.Builder
	for i := range slideCount {
		overrides.WriteString(fmt.Sprintf(`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, i+1))
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
  <Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>
  <Override PartName="/ppt/presProps.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presProps+xml"/>
  <Override PartName="/ppt/viewProps.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.viewProps+xml"/>
  <Override PartName="/ppt/tableStyles.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.tableStyles+xml"/>
  <Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>
  <Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>
  <Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>` + overrides.String() + `
</Types>`
}

func pptxRootRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>
</Relationships>`
}

func pptxPresentationRels(slideCount int) string {
	var rels strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/presProps" Target="presProps.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/viewProps" Target="viewProps.xml"/>
  <Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/tableStyles" Target="tableStyles.xml"/>`)
	for i := range slideCount {
		rels.WriteString(fmt.Sprintf(`  <Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, i+5, i+1))
	}
	rels.WriteString(`</Relationships>`)
	return rels.String()
}

func pptxPresentationXML(slideCount int) string {
	var ids strings.Builder
	for i := range slideCount {
		ids.WriteString(fmt.Sprintf(`<p:sldId id="%d" r:id="rId%d"/>`, 256+i, i+5))
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>
  <p:sldIdLst>` + ids.String() + `</p:sldIdLst>
  <p:sldSz cx="12192000" cy="6858000" type="screen16x9"/>
  <p:notesSz cx="6858000" cy="9144000"/>
</p:presentation>`
}

func pptxAppXML(slideCount int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>Slopr</Application><Slides>%d</Slides></Properties>`, slideCount)
}

func pptxCoreXML(title string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + xmlText(title) + `</dc:title><dc:creator>Slopr</dc:creator></cp:coreProperties>`
}

func pptxThemeXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Slopr"><a:themeElements><a:clrScheme name="Slopr"><a:dk1><a:srgbClr val="1D1D1B"/></a:dk1><a:lt1><a:srgbClr val="F3F0E8"/></a:lt1><a:dk2><a:srgbClr val="343432"/></a:dk2><a:lt2><a:srgbClr val="DEDAD0"/></a:lt2><a:accent1><a:srgbClr val="9A6B4F"/></a:accent1><a:accent2><a:srgbClr val="6F8B6B"/></a:accent2><a:accent3><a:srgbClr val="C7A35F"/></a:accent3><a:accent4><a:srgbClr val="7A7892"/></a:accent4><a:accent5><a:srgbClr val="A66F5F"/></a:accent5><a:accent6><a:srgbClr val="5F7F91"/></a:accent6><a:hlink><a:srgbClr val="4F6F9A"/></a:hlink><a:folHlink><a:srgbClr val="7A7892"/></a:folHlink></a:clrScheme><a:fontScheme name="Slopr"><a:majorFont><a:latin typeface="Aptos Display"/></a:majorFont><a:minorFont><a:latin typeface="Aptos"/></a:minorFont></a:fontScheme><a:fmtScheme name="Slopr"><a:fillStyleLst/><a:lnStyleLst/><a:effectStyleLst/><a:bgFillStyleLst/></a:fmtScheme></a:themeElements></a:theme>`
}

func pptxSlideMasterXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr></p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`
}

func pptxSlideLayoutXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" type="blank" preserve="1"><p:cSld name="Blank"><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr></p:spTree></p:cSld></p:sldLayout>`
}
