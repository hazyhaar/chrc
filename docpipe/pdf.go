// CLAUDE:SUMMARY Pure-Go PDF text extractor — decodes FlateDecode streams and parses Tj/TJ text operators.
package docpipe

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"
)

// extractPDF extracts text from a PDF file using pure Go.
// It reads the PDF structure, finds content streams, decompresses FlateDecode
// streams, and extracts text from PDF text operators (Tj, TJ, ', ").
func extractPDF(path string) (string, []Section, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}

	// Extract all text from content streams.
	allText := extractPDFText(data)
	if allText == "" {
		return "", nil, fmt.Errorf("no text content found in PDF")
	}

	// Split into sections by double-newlines or page breaks.
	paragraphs := splitPDFParagraphs(allText)

	var sections []Section
	var title string
	for _, p := range paragraphs {
		text := strings.TrimSpace(p)
		if text == "" {
			continue
		}
		if title == "" {
			title = text
			if len(title) > 200 {
				title = title[:200]
			}
		}
		sections = append(sections, Section{
			Text: text,
			Type: "paragraph",
		})
	}

	return title, sections, nil
}

// extractPDFText finds all content streams in the PDF and extracts text.
func extractPDFText(data []byte) string {
	var sb strings.Builder

	// Find all stream...endstream blocks.
	streamStart := []byte("stream")
	streamEnd := []byte("endstream")

	pos := 0
	for {
		idx := bytes.Index(data[pos:], streamStart)
		if idx < 0 {
			break
		}
		idx += pos + len(streamStart)

		// Skip whitespace after "stream" keyword.
		for idx < len(data) && (data[idx] == '\r' || data[idx] == '\n') {
			idx++
		}

		endIdx := bytes.Index(data[idx:], streamEnd)
		if endIdx < 0 {
			break
		}
		endIdx += idx

		streamData := data[idx:endIdx]
		pos = endIdx + len(streamEnd)

		// Try to decompress (FlateDecode is the most common).
		decoded := tryDecompress(streamData)
		text := extractTextFromStream(decoded)
		if text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(text)
		}
	}

	return sb.String()
}

// tryDecompress attempts FlateDecode decompression. Returns raw data on failure.
func tryDecompress(data []byte) []byte {
	reader := flate.NewReader(bytes.NewReader(data))
	decoded, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || len(decoded) == 0 {
		return data // Not compressed or error — return raw.
	}
	return decoded
}

// pdfStringRe matches PDF string literals in parentheses: (text here)
var pdfStringRe = regexp.MustCompile(`\(([^)]*)\)`)

// extractTextFromStream parses PDF content stream operators for text.
func extractTextFromStream(data []byte) string {
	var sb strings.Builder

	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// Tj operator: (text) Tj
		if bytes.HasSuffix(line, []byte("Tj")) {
			matches := pdfStringRe.FindAllSubmatch(line, -1)
			for _, m := range matches {
				text := decodePDFString(m[1])
				if text != "" {
					sb.WriteString(text)
				}
			}
		}

		// TJ operator: [(text) -100 (more text)] TJ
		if bytes.HasSuffix(line, []byte("TJ")) {
			matches := pdfStringRe.FindAllSubmatch(line, -1)
			for _, m := range matches {
				text := decodePDFString(m[1])
				if text != "" {
					sb.WriteString(text)
				}
			}
		}

		// ' operator (move to next line and show text): (text) '
		if bytes.HasSuffix(line, []byte("'")) && bytes.Contains(line, []byte("(")) {
			matches := pdfStringRe.FindAllSubmatch(line, -1)
			for _, m := range matches {
				text := decodePDFString(m[1])
				if text != "" {
					sb.WriteByte('\n')
					sb.WriteString(text)
				}
			}
		}

		// Td/TD operator (text positioning — add space/newline).
		if bytes.HasSuffix(line, []byte("Td")) || bytes.HasSuffix(line, []byte("TD")) {
			if sb.Len() > 0 {
				sb.WriteByte(' ')
			}
		}

		// T* operator (move to start of next line).
		if bytes.Equal(line, []byte("T*")) {
			sb.WriteByte('\n')
		}
	}

	return cleanPDFText(sb.String())
}

// decodePDFString handles basic PDF escape sequences.
func decodePDFString(raw []byte) string {
	var sb strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
			switch raw[i] {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case '\\':
				sb.WriteByte('\\')
			case '(':
				sb.WriteByte('(')
			case ')':
				sb.WriteByte(')')
			default:
				// Octal escape (e.g. \040 for space).
				if raw[i] >= '0' && raw[i] <= '7' {
					val := int(raw[i] - '0')
					if i+1 < len(raw) && raw[i+1] >= '0' && raw[i+1] <= '7' {
						i++
						val = val*8 + int(raw[i]-'0')
						if i+1 < len(raw) && raw[i+1] >= '0' && raw[i+1] <= '7' {
							i++
							val = val*8 + int(raw[i]-'0')
						}
					}
					sb.WriteByte(byte(val))
				} else {
					sb.WriteByte(raw[i])
				}
			}
		} else {
			sb.WriteByte(raw[i])
		}
	}
	return sb.String()
}

// cleanPDFText normalises whitespace in extracted PDF text.
func cleanPDFText(text string) string {
	var sb strings.Builder
	prevSpace := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if !prevSpace && sb.Len() > 0 {
				sb.WriteByte(' ')
				prevSpace = true
			}
		} else if unicode.IsPrint(r) {
			sb.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(sb.String())
}

// splitPDFParagraphs splits text on double-newlines or multiple spaces.
func splitPDFParagraphs(text string) []string {
	// Normalise line endings.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	parts := strings.Split(text, "\n\n")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 && text != "" {
		result = []string{text}
	}
	return result
}
