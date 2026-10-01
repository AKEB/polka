package library

import (
	"html"
	"strings"
	"unicode/utf8"
)

// charsPerPage is a rough printed-page size for fiction
// (≈30 lines × 60 characters with spaces).
const charsPerPage = 1800

// EstimatePages returns an approximate page count from chapter HTML.
func EstimatePages(text *FB2Text) int {
	if text == nil {
		return 0
	}
	n := 0
	for _, ch := range text.Chapters {
		n += plainTextLen(ch.HTML)
	}
	if n <= 0 {
		return 0
	}
	pages := (n + charsPerPage - 1) / charsPerPage
	if pages < 1 {
		pages = 1
	}
	return pages
}

func plainTextLen(s string) int {
	if s == "" {
		return 0
	}
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	plain := html.UnescapeString(b.String())
	plain = strings.Join(strings.Fields(plain), " ")
	return utf8.RuneCountInString(plain)
}
