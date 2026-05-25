package extractor

import (
	"bytes"
	"os"
	"strings"

	"golang.org/x/net/html"
)

// HTMLExtractor handles .html and .htm files.
// Strips tags and skips script/style content.
type HTMLExtractor struct{}

func (e *HTMLExtractor) Extract(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	walkHTML(doc, &buf)
	return strings.TrimSpace(buf.String()), nil
}

// noTextTags are elements whose subtree we skip entirely.
var noTextTags = map[string]bool{
	"script": true,
	"style":  true,
	"head":   true,
}

// blockTags get a trailing newline so paragraphs don't merge into one line.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"li": true, "tr": true, "blockquote": true,
}

func walkHTML(n *html.Node, buf *bytes.Buffer) {
	if n.Type == html.ElementNode && noTextTags[n.Data] {
		return
	}

	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			buf.WriteString(text)
			buf.WriteByte(' ')
		}
		return
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTML(c, buf)
	}

	if n.Type == html.ElementNode && blockTags[n.Data] {
		buf.WriteByte('\n')
	}
}
