package extract

import (
	"regexp"
	"strings"
	"unicode"
)

// CleanText normalises extracted text for storage and search.
// It collapses whitespace, removes zero-width characters, and trims.
func CleanText(text string) string {
	// Remove zero-width characters.
	text = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff', '\u00ad':
			return -1
		}
		return r
	}, text)

	// Collapse multiple whitespace to single space.
	text = collapseWhitespace(text)

	// Trim leading/trailing whitespace.
	text = strings.TrimSpace(text)

	return text
}

// NormaliseForHash prepares text for content-hash comparison.
// More aggressive than CleanText: lowercases, removes punctuation.
func NormaliseForHash(text string) string {
	text = CleanText(text)
	text = strings.ToLower(text)
	text = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, text)
	return collapseWhitespace(text)
}

var multiSpaceRe = regexp.MustCompile(`\s+`)

func collapseWhitespace(s string) string {
	return multiSpaceRe.ReplaceAllString(s, " ")
}

// SplitParagraphs splits text into paragraphs (double newline boundaries).
func SplitParagraphs(text string) []string {
	parts := regexp.MustCompile(`\n\s*\n`).Split(text, -1)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
