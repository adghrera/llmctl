package extract

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"strconv"
	"strings"
	"testing"
)

func TestTextPlainPassthrough(t *testing.T) {
	got, err := Text("notes.txt", []byte("hello world"))
	if err != nil || got != "hello world" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestTextDOCX(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(`<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:t xml:space="preserve"> world</w:t></w:r></w:p>
<w:p><w:r><w:t>Second &amp; paragraph</w:t></w:r></w:p>
<w:p><w:r><w:t>A</w:t><w:tab/><w:t>B</w:t></w:r></w:p>
</w:body></w:document>`))
	zw.Close()

	got, err := Text("essay.docx", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Hello world", "Second & paragraph", "A\tB"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Count(got, "\n") < 2 {
		t.Errorf("expected paragraph breaks in:\n%s", got)
	}
}

func TestTextDOCXInvalid(t *testing.T) {
	if _, err := Text("x.docx", []byte("not a zip")); err == nil {
		t.Error("expected error for non-zip docx")
	}
	// Valid zip without document.xml.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("other.xml")
	w.Write([]byte("<x/>"))
	zw.Close()
	if _, err := Text("x.docx", buf.Bytes()); err == nil {
		t.Error("expected error for missing document.xml")
	}
}

// buildPDF makes a minimal PDF with one zlib-compressed content stream.
func buildPDF(t *testing.T, ops string) []byte {
	t.Helper()
	var comp bytes.Buffer
	zw := zlib.NewWriter(&comp)
	zw.Write([]byte(ops))
	zw.Close()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	buf.WriteString("1 0 obj\n<< /Length " + strconv.Itoa(comp.Len()) + " /Filter /FlateDecode >>\nstream\n")
	buf.Write(comp.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	buf.WriteString("trailer\n<< >>\n%%EOF\n")
	return buf.Bytes()
}

func TestTextPDF(t *testing.T) {
	ops := "BT /F1 12 Tf 72 720 Td (Hello from PDF) Tj 0 -14 Td (Second [line] (parens)) Tj ET"
	pdf := buildPDF(t, ops)
	got, err := Text("doc.pdf", pdf)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Hello from PDF", "Second [line] (parens)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestTextPDFUncompressedStream(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	ops := "BT (Plain stream) Tj ET"
	buf.WriteString("1 0 obj\nstream\n" + ops + "\nendstream\nendobj\n%%EOF")
	got, err := Text("p.pdf", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Plain stream") {
		t.Errorf("got:\n%s", got)
	}
}

func TestTextPDFNotAPDF(t *testing.T) {
	if _, err := Text("x.pdf", []byte("JPEG data")); err == nil {
		t.Error("expected error for non-PDF")
	}
}

func TestTextPDFNoText(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n1 0 obj\nstream\n/GS0 gs\nendstream\nendobj\n%%EOF")
	if _, err := Text("img.pdf", buf.Bytes()); err == nil {
		t.Error("expected error when no text found")
	}
}

func TestSupported(t *testing.T) {
	if !Supported("a.PDF") || !Supported("b.docx") || Supported("c.txt") {
		t.Error("Supported misclassified")
	}
}
