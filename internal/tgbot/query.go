package tgbot

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vestigiumincaligne/polka/internal/store"
)

// ParseQuery turns a Calibre-style query into AND filters.
//
//	title:Harry          — title contains
//	title:=Harry Potter  — exact title
//	author:Rowling and series:Potter
//	plain text           — full-text search
func ParseQuery(raw string) []store.SearchFilter {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := splitAnd(raw)
	var out []store.SearchFilter
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		field, exact, value, ok := splitField(p)
		if !ok {
			out = append(out, store.SearchFilter{Value: p})
			continue
		}
		out = append(out, store.SearchFilter{Field: field, Exact: exact, Value: value})
	}
	return out
}

func splitAnd(s string) []string {
	lower := strings.ToLower(s)
	var parts []string
	start := 0
	for i := 0; i < len(s); {
		if strings.HasPrefix(lower[i:], " and ") {
			parts = append(parts, s[start:i])
			i += len(" and ")
			start = i
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	parts = append(parts, s[start:])
	return parts
}

func splitField(p string) (field string, exact bool, value string, ok bool) {
	colon := strings.IndexByte(p, ':')
	if colon <= 0 {
		return "", false, "", false
	}
	field = strings.ToLower(strings.TrimSpace(p[:colon]))
	if field == "" || !isFieldName(field) {
		return "", false, "", false
	}
	rest := strings.TrimSpace(p[colon+1:])
	if strings.HasPrefix(rest, "=") {
		exact = true
		rest = strings.TrimSpace(rest[1:])
	}
	if rest == "" {
		return "", false, "", false
	}
	return field, exact, rest, true
}

func isFieldName(s string) bool {
	switch s {
	case "title", "author", "series", "tags", "tag", "genre", "genres",
		"languages", "language", "lang", "formats", "format", "ext",
		"publisher", "search", "any":
		return true
	}
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return false
}
