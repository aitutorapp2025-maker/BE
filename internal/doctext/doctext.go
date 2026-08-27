// Package doctext extracts plain text from student-uploaded documents that
// vision models can't read directly: Word (.docx), PowerPoint (.pptx) and
// plain-text files. The extracted text is folded into the homework prompt.
// Legacy binary formats (.doc/.ppt) are not supported — callers should tell
// the student to re-save as PDF or DOCX.
package doctext

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// MaxChars caps how much extracted text is fed to the AI prompt.
const MaxChars = 12000

var tagRe = regexp.MustCompile(`<[^>]+>`)

// Supported reports whether a media type can be turned into text here.
func Supported(mediaType string) bool {
	switch normalize(mediaType) {
	case "docx", "pptx", "txt":
		return true
	}
	return false
}

// normalize maps a MIME type (or extension-ish string) to docx/pptx/txt/"".
func normalize(mediaType string) string {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	switch {
	case strings.Contains(mt, "wordprocessingml"), strings.HasSuffix(mt, "docx"):
		return "docx"
	case strings.Contains(mt, "presentationml"), strings.HasSuffix(mt, "pptx"):
		return "pptx"
	case strings.HasPrefix(mt, "text/"), strings.HasSuffix(mt, "txt"):
		return "txt"
	}
	return ""
}

// Extract returns the plain text of a supported document.
func Extract(data []byte, mediaType string) (string, error) {
	switch normalize(mediaType) {
	case "txt":
		if !utf8.Valid(data) {
			return "", fmt.Errorf("that text file is not readable — please save it as UTF-8 or upload a PDF")
		}
		return clip(string(data)), nil
	case "docx":
		return extractZipXML(data, func(name string) bool {
			return name == "word/document.xml"
		})
	case "pptx":
		return extractZipXML(data, func(name string) bool {
			return strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml")
		})
	}
	return "", fmt.Errorf("unsupported document type")
}

// extractZipXML pulls the matching XML parts out of an OOXML zip and strips
// the markup, keeping paragraph-ish breaks.
func extractZipXML(data []byte, match func(name string) bool) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("could not read the document — please re-save it as PDF or DOCX")
	}
	names := make([]string, 0, 4)
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		if match(f.Name) {
			names = append(names, f.Name)
			byName[f.Name] = f
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("the document appears to be empty")
	}
	sort.Strings(names) // slide1, slide10 ordering is imperfect but fine for text

	var out strings.Builder
	for _, name := range names {
		rc, err := byName[name].Open()
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		rc.Close()
		if err != nil {
			continue
		}
		s := string(raw)
		// Turn paragraph/break tags into newlines before stripping the rest.
		for _, br := range []string{"</w:p>", "</a:p>", "<w:br/>", "<a:br/>"} {
			s = strings.ReplaceAll(s, br, br+"\n")
		}
		s = tagRe.ReplaceAllString(s, "")
		out.WriteString(strings.TrimSpace(s))
		out.WriteString("\n\n")
		if out.Len() > MaxChars {
			break
		}
	}
	text := clip(strings.TrimSpace(out.String()))
	if text == "" {
		return "", fmt.Errorf("no readable text found in the document")
	}
	return text, nil
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > MaxChars {
		s = s[:MaxChars]
	}
	return s
}
