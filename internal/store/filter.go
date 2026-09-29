package store

import (
	"context"
	"strings"
	"unicode"
)

// LangUnknown is the filter value for books with an empty lang column.
const LangUnknown = "-"

type langCtxKey struct{}

// WithLang restricts book listings in ctx to a catalog language code.
// An empty code means all languages. "-" (LangUnknown) means books
// whose lang is empty.
func WithLang(ctx context.Context, lang string) context.Context {
	lang = NormalizeLang(lang)
	if lang == "" {
		return ctx
	}
	return context.WithValue(ctx, langCtxKey{}, lang)
}

// NormalizeLang lowercases a catalog language code and accepts "-" /
// "unknown" / "und" as LangUnknown. Anything else is kept if it looks
// like an ISO code (2–8 letters).
func NormalizeLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "all", "*":
		return ""
	case "-", "unknown", "und", "?":
		return LangUnknown
	}
	if len(s) < 2 || len(s) > 8 {
		return ""
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && r != '-' {
			return ""
		}
	}
	return s
}

func langFrom(ctx context.Context) string {
	s, _ := ctx.Value(langCtxKey{}).(string)
	return s
}

// langSQL is a fragment starting with AND, plus bind args, or empty.
func langSQL(ctx context.Context) (string, []any) {
	switch langFrom(ctx) {
	case "":
		return "", nil
	case LangUnknown:
		return ` AND b.lang = ''`, nil
	default:
		return ` AND b.lang = ?`, []any{langFrom(ctx)}
	}
}

// LangCount is one language in the catalog, with a live-book count.
type LangCount struct {
	Code  string
	Books int
}

// Languages lists catalog languages of non-deleted books, most common first.
func (s *Store) Languages(ctx context.Context) ([]LangCount, error) {
	return s.languagesWhere(ctx, "")
}

// LanguagesForShelf counts languages in a built-in or genre shelf.
// The current language filter is ignored so chip counts stay complete.
func (s *Store) LanguagesForShelf(ctx context.Context, shelfID string) ([]LangCount, error) {
	if code, isGenre := strings.CutPrefix(shelfID, "genre_"); isGenre {
		return s.languagesWhere(ctx,
			`b.id IN (SELECT book_id FROM book_genres bg JOIN genres g ON g.id = bg.genre_id WHERE g.code = ?)`,
			code)
	}
	where, _, ok := shelfWhere(shelfID)
	if !ok {
		return nil, ErrNotFound
	}
	return s.languagesWhere(ctx, where)
}

// LanguagesForAuthor counts languages among an author's live books.
func (s *Store) LanguagesForAuthor(ctx context.Context, authorID int64) ([]LangCount, error) {
	return s.languagesWhere(ctx, `b.id IN (SELECT book_id FROM book_authors WHERE author_id = ?)`, authorID)
}

// LanguagesForSeries counts languages in a series.
func (s *Store) LanguagesForSeries(ctx context.Context, seriesID int64) ([]LangCount, error) {
	return s.languagesWhere(ctx, `b.series_id = ?`, seriesID)
}

// LanguagesForIDs counts languages among the given book ids.
func (s *Store) LanguagesForIDs(ctx context.Context, ids []int64) ([]LangCount, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return s.languagesWhere(ctx, `b.id IN (`+placeholders+`)`, args...)
}

func (s *Store) languagesWhere(ctx context.Context, where string, args ...any) ([]LangCount, error) {
	q := `
		SELECT CASE WHEN b.lang = '' THEN '-' ELSE b.lang END, count(*)
		FROM books b
		WHERE b.deleted = 0`
	if where != "" {
		q += ` AND ` + where
	}
	q += `
		GROUP BY 1
		HAVING count(*) > 0
		ORDER BY count(*) DESC, 1`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LangCount
	for rows.Next() {
		var l LangCount
		if err := rows.Scan(&l.Code, &l.Books); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
