package doctext

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func makeZip(t *testing.T, name, content string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDocx(t *testing.T) {
	docx := makeZip(t, "word/document.xml",
		`<w:document><w:body><w:p><w:r><w:t>Solve ten sums</w:t></w:r></w:p><w:p><w:r><w:t>Read chapter four</w:t></w:r></w:p></w:body></w:document>`)
	text, err := Extract(docx, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Solve ten sums") || !strings.Contains(text, "Read chapter four") {
		t.Fatalf("missing text: %q", text)
	}
}

func TestPptx(t *testing.T) {
	pptx := makeZip(t, "ppt/slides/slide1.xml",
		`<p:sld><a:p><a:r><a:t>Photosynthesis basics</a:t></a:r></a:p></p:sld>`)
	text, err := Extract(pptx, "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Photosynthesis basics") {
		t.Fatalf("missing text: %q", text)
	}
}

func TestTxtAndUnsupported(t *testing.T) {
	text, err := Extract([]byte("plain homework text"), "text/plain")
	if err != nil || text != "plain homework text" {
		t.Fatalf("txt failed: %q %v", text, err)
	}
	if Supported("application/msword") {
		t.Fatal("legacy .doc must not be supported")
	}
	if _, err := Extract([]byte("junk"), "application/msword"); err == nil {
		t.Fatal("expected error for unsupported type")
	}
}
