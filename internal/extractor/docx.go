package extractor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// DocxExtractor handles .docx files.
// A docx is a zip archive containing word/document.xml — no external dep needed.
type DocxExtractor struct{}

func (e *DocxExtractor) Extract(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("cannot open docx: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("cannot read document.xml: %w", err)
		}
		defer rc.Close()

		data, err := io.ReadAll(rc)
		if err != nil {
			return "", err
		}
		return parseDocumentXML(data)
	}
	return "", fmt.Errorf("word/document.xml not found in %s", path)
}

// parseDocumentXML walks the XML token stream and collects <w:t> text runs,
// inserting newlines at paragraph boundaries (<w:p>).
func parseDocumentXML(data []byte) (string, error) {
	var buf bytes.Buffer
	dec := xml.NewDecoder(bytes.NewReader(data))
	inTextRun := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inTextRun = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inTextRun = false
			}
			if t.Name.Local == "p" {
				buf.WriteByte('\n')
			}
		case xml.CharData:
			if inTextRun {
				buf.Write(t)
			}
		}
	}
	return strings.TrimSpace(buf.String()), nil
}
