// Package extract pulls plain text out of uploaded documents using only the
// standard library. DOCX (a zip with word/document.xml) extracts reliably.
// PDF extraction is best-effort: it inflates FlateDecode content streams and
// reads the text-drawing operators; PDFs with embedded/subset font encodings
// may produce garbled text.
package extract

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Supported reports whether Text() can extract from this filename.
func Supported(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf", ".docx":
		return true
	}
	return false
}

// Text extracts plain text from a document based on its extension. For
// unsupported extensions it returns the content decoded as UTF-8 (with
// invalid bytes replaced), so callers can use it uniformly.
func Text(filename string, content []byte) (string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".docx":
		return docxText(content)
	case ".pdf":
		return pdfText(content)
	default:
		return string(toUTF8(content)), nil
	}
}

func toUTF8(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	var sb strings.Builder
	for _, r := range string(b) {
		sb.WriteRune(r) // range over string replaces invalid bytes with U+FFFD
	}
	return []byte(sb.String())
}

// ---- DOCX ----

// docxText reads word/document.xml from the .docx zip and walks its XML,
// collecting text runs and inserting newlines at paragraph ends.
func docxText(content []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", fmt.Errorf("extract: not a valid .docx (zip): %w", err)
	}
	var doc io.ReadCloser
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc, err = f.Open()
			if err != nil {
				return "", err
			}
			defer doc.Close()
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("extract: word/document.xml not found in .docx")
	}
	dec := xml.NewDecoder(doc)
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("extract: document.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tab":
				sb.WriteByte('\t')
			case "br", "cr":
				sb.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				sb.WriteByte('\n')
			}
		case xml.CharData:
			sb.Write([]byte(t))
		}
	}
	return normalize(sb.String()), nil
}

// ---- PDF (best effort) ----

// pdfText scans for stream objects, inflates them when compressed, and pulls
// text out of the Tj / TJ / ' / " text operators.
func pdfText(content []byte) (string, error) {
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return "", fmt.Errorf("extract: not a PDF (missing %%PDF- header)")
	}
	var sb strings.Builder
	rest := content
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			break
		}
		rest = rest[i+len("stream"):]
		// Stream data starts after a CRLF or LF.
		if bytes.HasPrefix(rest, []byte("\r\n")) {
			rest = rest[2:]
		} else if len(rest) > 0 && rest[0] == '\n' {
			rest = rest[1:]
		}
		j := bytes.Index(rest, []byte("endstream"))
		if j < 0 {
			break
		}
		raw := rest[:j]
		rest = rest[j+len("endstream"):]

		data := inflateMaybe(raw)
		extractPDFOps(data, &sb)
	}
	out := normalize(sb.String())
	if out == "" {
		return "", fmt.Errorf("extract: no extractable text found in PDF (it may be scanned or use embedded font encodings)")
	}
	return out, nil
}

// inflateMaybe zlib-inflates raw; on failure returns raw unchanged (streams
// may be uncompressed or use other filters we skip).
func inflateMaybe(raw []byte) []byte {
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, 64<<20))
	if err != nil || len(out) == 0 {
		return raw
	}
	return out
}

// extractPDFOps scans a content stream for text-showing operators and
// appends the string operands.
func extractPDFOps(data []byte, sb *strings.Builder) {
	// Only bother with streams that look like content streams.
	if !bytes.Contains(data, []byte("Tj")) && !bytes.Contains(data, []byte("TJ")) {
		return
	}
	i := 0
	for i < len(data) {
		c := data[i]
		if c == '(' {
			s, next := parsePDFString(data, i)
			sb.WriteString(s)
			i = next
			continue
		}
		if c == '<' && i+1 < len(data) && data[i+1] != '<' {
			// Hex string <...>
			end := bytes.IndexByte(data[i+1:], '>')
			if end > 0 {
				hex := data[i+1 : i+1+end]
				if s, ok := decodeHex(hex); ok {
					sb.WriteString(s)
					i = i + 2 + end
					continue
				}
			}
		}
		if c == '[' {
			// TJ array: [ (a) -2 (b) ] — collect the strings inside.
			end := findArrayEnd(data, i)
			if end > i {
				inner := data[i:end]
				k := 0
				for k < len(inner) {
					if inner[k] == '(' {
						s, next := parsePDFString(inner, k)
						sb.WriteString(s)
						k = next
						continue
					}
					k++
				}
				sb.WriteByte(' ')
				i = end + 1
				continue
			}
		}
		// ' and " operators imply a following newline in text mode; treat
		// EOL-ish operators loosely: a Tj/TJ/'/" outside a string ends a line.
		if (c == 'T' || c == '\'' || c == '"') && i+1 < len(data) {
			if c != 'T' || (i+2 < len(data) && data[i+1] == 'j' && (data[i+2] < 'a' || data[i+2] > 'z')) {
				sb.WriteByte('\n')
			}
		}
		i++
	}
}

// parsePDFString parses a (...) literal starting at data[i]=='(' and returns
// the decoded text and the index just past the closing paren.
func parsePDFString(data []byte, i int) (string, int) {
	i++ // skip opening '('
	depth := 1
	var sb strings.Builder
	for i < len(data) {
		c := data[i]
		switch c {
		case '\\':
			if i+1 >= len(data) {
				return sb.String(), i + 1
			}
			n := data[i+1]
			switch n {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case '(', ')', '\\':
				sb.WriteByte(n)
			default:
				if n >= '0' && n <= '7' { // octal, up to 3 digits
					val := 0
					digits := 0
					for digits < 3 && i+1+digits < len(data) && data[i+1+digits] >= '0' && data[i+1+digits] <= '7' {
						val = val*8 + int(data[i+1+digits]-'0')
						digits++
					}
					sb.WriteByte(byte(val))
					i += digits
				} else {
					sb.WriteByte(n)
				}
			}
			i += 2
		case '(':
			depth++
			sb.WriteByte(c)
			i++
		case ')':
			depth--
			if depth == 0 {
				return sb.String(), i + 1
			}
			sb.WriteByte(c)
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String(), i
}

func findArrayEnd(data []byte, i int) int {
	depth := 0
	for j := i; j < len(data); j++ {
		switch data[j] {
		case '(':
			// skip string contents so parens inside don't confuse us
			_, next := parsePDFString(data, j)
			j = next - 1
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

func decodeHex(hex []byte) (string, bool) {
	var sb strings.Builder
	for i := 0; i+1 < len(hex); i += 2 {
		hi, ok1 := hexVal(hex[i])
		lo, ok2 := hexVal(hex[i+1])
		if !ok1 || !ok2 {
			return "", false
		}
		sb.WriteByte(hi<<4 | lo)
	}
	return sb.String(), true
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// normalize collapses excessive blank lines and trims.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}
