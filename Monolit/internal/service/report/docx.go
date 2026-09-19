package report

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

func generateDOCXReport(data ReportData) ([]byte, error) {
	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)

	files := map[string]string{
		"[Content_Types].xml": contentTypesXML,
		"_rels/.rels":         relsXML,
		"word/document.xml":   documentXML(data),
	}

	for name, content := range files {
		writer, err := zipWriter.Create(name)
		if err != nil {
			return nil, fmt.Errorf("create docx part: %w", err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			return nil, fmt.Errorf("write docx part: %w", err)
		}
	}

	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close docx: %w", err)
	}

	return buffer.Bytes(), nil
}

func documentXML(data ReportData) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	docxHeading(&b, data.Title(), 36)
	docxParagraph(&b, "Статус звонка: "+string(data.Call.Status))
	docxParagraph(&b, fmt.Sprintf("Длительность: %d сек.", data.Call.DurationSeconds))
	docxParagraph(&b, "Создан: "+data.Call.CreatedAt.Format(timeLayout))
	docxParagraph(&b, "Отчет создан: "+data.GeneratedAt.Format(timeLayout))
	docxParagraph(&b, "")
	if !data.TranscriptionOnly {
		docxParagraph(&b, "Анализ")
		docxParagraph(&b, "Статус анализа: "+string(data.Analysis.Status))
	}

	for _, section := range data.Sections() {
		docxParagraph(&b, "")
		docxHeading(&b, section.Title, 26)
		for _, row := range section.Rows {
			if row.Label != "" && row.Value != "" {
				docxParagraph(&b, row.Label+": "+row.Value)
			} else if row.Value != "" {
				for _, paragraph := range splitParagraphs(row.Value) {
					if strings.HasPrefix(section.Title, "Транскрипция") {
						docxTranscriptParagraph(&b, paragraph)
					} else {
						docxParagraph(&b, paragraph)
					}
				}
			} else if row.Label != "" {
				docxParagraph(&b, row.Label+":")
			}
			for _, item := range row.List {
				docxParagraph(&b, "• "+item)
			}
		}
	}

	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

func docxParagraph(b *strings.Builder, text string) {
	b.WriteString(`<w:p><w:pPr><w:spacing w:after="120" w:line="300" w:lineRule="auto"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="22"/></w:rPr><w:t xml:space="preserve">`)
	b.WriteString(xmlEscape(text))
	b.WriteString(`</w:t></w:r></w:p>`)
}

func xmlEscape(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func splitParagraphs(value string) []string {
	return strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

const relsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

func docxHeading(b *strings.Builder, text string, size int) {
	fmt.Fprintf(b, `<w:p><w:pPr><w:keepNext/><w:shd w:fill="FFF0E7"/><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="E56536"/></w:pBdr><w:spacing w:before="240" w:after="180"/></w:pPr><w:r><w:rPr><w:b/><w:color w:val="263449"/><w:sz w:val="%d"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, size, xmlEscape(text))
}

var transcriptHeading = regexp.MustCompile(`^.+ · \d{2,}:\d{2}(?::\d{2})? – \d{2,}:\d{2}(?::\d{2})?$`)

func docxTranscriptParagraph(b *strings.Builder, text string) {
	if strings.TrimSpace(text) == "" {
		docxParagraph(b, "")
		return
	}
	fill, color, emphasis := "F6F8FB", "263449", ""
	keepNext := ""
	if transcriptHeading.MatchString(text) {
		fill, color, emphasis, keepNext = "FFF0E7", "9D3A19", "<w:b/>", "<w:keepNext/>"
	}
	fmt.Fprintf(b, `<w:p><w:pPr>%s<w:shd w:fill="%s"/><w:pBdr><w:left w:val="single" w:sz="12" w:space="8" w:color="E56536"/></w:pBdr><w:spacing w:after="100" w:line="300" w:lineRule="auto"/><w:ind w:left="160" w:right="160"/></w:pPr><w:r><w:rPr>%s<w:rFonts w:ascii="Calibri" w:hAnsi="Calibri"/><w:color w:val="%s"/><w:sz w:val="22"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, keepNext, fill, emphasis, color, xmlEscape(text))
}
