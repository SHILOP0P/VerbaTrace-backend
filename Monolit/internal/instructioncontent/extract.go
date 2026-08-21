package instructioncontent

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	pdf "github.com/ledongthuc/pdf"
)

func Extract(filename string, data []byte) (string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md":
		return string(data), nil
	case ".pdf":
		reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("open pdf: %w", err)
		}
		plain, err := reader.GetPlainText()
		if err != nil {
			return "", fmt.Errorf("extract pdf text: %w", err)
		}
		result, err := io.ReadAll(plain)
		return string(result), err
	case ".docx":
		return extractDOCX(data)
	case ".xlsx":
		return extractXLSX(data)
	default:
		return "", fmt.Errorf("unsupported instruction format")
	}
}

func zipFile(data []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, file := range zr.File {
		if file.Name != name {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return io.ReadAll(r)
	}
	return nil, fmt.Errorf("archive entry %s not found", name)
}

func extractDOCX(data []byte) (string, error) {
	doc, err := zipFile(data, "word/document.xml")
	if err != nil {
		return "", err
	}
	decoder := xml.NewDecoder(bytes.NewReader(doc))
	var out strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "t" {
				var text string
				if err = decoder.DecodeElement(&text, &value); err != nil {
					return "", err
				}
				out.WriteString(text)
			}
			if value.Name.Local == "tab" {
				out.WriteByte('\t')
			}
		case xml.EndElement:
			if value.Name.Local == "p" {
				out.WriteByte('\n')
			}
			if value.Name.Local == "tr" {
				out.WriteByte('\n')
			}
			if value.Name.Local == "tc" {
				out.WriteByte('\t')
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func extractXLSX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	shared := []string{}
	if payload, e := zipFile(data, "xl/sharedStrings.xml"); e == nil {
		decoder := xml.NewDecoder(bytes.NewReader(payload))
		for {
			token, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", e
			}
			if start, ok := token.(xml.StartElement); ok && start.Name.Local == "si" {
				var item struct {
					Text string `xml:"t"`
					Runs []struct {
						Text string `xml:"t"`
					} `xml:"r"`
				}
				if e = decoder.DecodeElement(&item, &start); e != nil {
					return "", e
				}
				text := item.Text
				for _, run := range item.Runs {
					text += run.Text
				}
				shared = append(shared, text)
			}
		}
	}
	var out strings.Builder
	for _, file := range zr.File {
		if !strings.HasPrefix(file.Name, "xl/worksheets/sheet") || !strings.HasSuffix(file.Name, ".xml") {
			continue
		}
		out.WriteString("\n## " + strings.TrimSuffix(filepath.Base(file.Name), ".xml") + "\n")
		r, e := file.Open()
		if e != nil {
			return "", e
		}
		decoder := xml.NewDecoder(r)
		for {
			token, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				_ = r.Close()
				return "", e
			}
			start, ok := token.(xml.StartElement)
			if !ok || start.Name.Local != "c" {
				continue
			}
			var cell struct {
				Type   string `xml:"t,attr"`
				Value  string `xml:"v"`
				Inline string `xml:"is>t"`
			}
			if e = decoder.DecodeElement(&cell, &start); e != nil {
				_ = r.Close()
				return "", e
			}
			value := cell.Value
			switch cell.Type {
			case "s":
				if i, parseErr := strconv.Atoi(value); parseErr == nil && i >= 0 && i < len(shared) {
					value = shared[i]
				}
			case "inlineStr":
				value = cell.Inline
			}
			out.WriteString(value + "\t")
		}
		_ = r.Close()
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String()), nil
}
