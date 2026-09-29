package store

import (
	"context"
	"fmt"
	"strings"
)

// SearchFilter is one AND-clause for advanced / Telegram search.
// Field is one of: title, author, series, genre, lang, ext, or "" for
// full-text across title/authors/series.
type SearchFilter struct {
	Field string
	Exact bool
	Value string
}

// SearchFiltered returns books matching all filters, plus the total count.
func (s *Store) SearchFiltered(ctx context.Context, filters []SearchFilter, limit, offset int) ([]Book, int, error) {
	where, args, err := buildFilterSQL(filters)
	if err != nil {
		return nil, 0, err
	}
	if extra, extraArgs := langSQL(ctx); extra != "" {
		where += extra
		args = append(args, extraArgs...)
	}

	var total int
	countQ := `SELECT count(*) FROM books b LEFT JOIN series s ON s.id = b.series_id WHERE b.deleted = 0` + where
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 || limit == 0 {
		return nil, total, nil
	}

	q := `SELECT` + bookColumns + bookFrom + ` WHERE b.deleted = 0` + where +
		` ORDER BY b.lib_rate DESC, b.title LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	books, err := s.scanBooks(ctx, q, args...)
	return books, total, err
}

// RandomBooks returns up to limit random live books.
func (s *Store) RandomBooks(ctx context.Context, limit int) ([]Book, error) {
	if limit <= 0 {
		limit = 10
	}
	extra, extraArgs := langSQL(ctx)
	args := append(append([]any{}, extraArgs...), limit)
	return s.scanBooks(ctx, `SELECT`+bookColumns+bookFrom+
		` WHERE b.deleted = 0`+extra+` ORDER BY random() LIMIT ?`, args...)
}

func buildFilterSQL(filters []SearchFilter) (string, []any, error) {
	var where strings.Builder
	var args []any
	for _, f := range filters {
		v := strings.TrimSpace(f.Value)
		if v == "" {
			continue
		}
		field := strings.ToLower(strings.TrimSpace(f.Field))
		switch field {
		case "", "search", "any":
			q := ftsQuery(v, "")
			if q == "" {
				continue
			}
			where.WriteString(` AND b.id IN (SELECT rowid FROM book_search WHERE book_search MATCH ?)`)
			args = append(args, q)
		case "title":
			if f.Exact {
				where.WriteString(` AND lower(b.title) = lower(?)`)
				args = append(args, v)
			} else {
				q := ftsQuery(v, "title")
				if q == "" {
					continue
				}
				where.WriteString(` AND b.id IN (SELECT rowid FROM book_search WHERE book_search MATCH ?)`)
				args = append(args, q)
			}
		case "author":
			if f.Exact {
				where.WriteString(` AND b.id IN (
					SELECT ba.book_id FROM book_authors ba
					JOIN authors a ON a.id = ba.author_id
					WHERE lower(trim(a.last_name || ' ' || a.first_name || ' ' || a.middle_name)) = lower(?)
					   OR lower(trim(a.first_name || ' ' || a.last_name)) = lower(?)
					   OR lower(a.last_name) = lower(?))`)
				args = append(args, v, v, v)
			} else {
				where.WriteString(` AND b.id IN (
					SELECT ba.book_id FROM book_authors ba
					JOIN authors a ON a.id = ba.author_id
					WHERE lower(a.last_name || ' ' || a.first_name || ' ' || a.middle_name) LIKE lower(?)
					   OR lower(a.last_name) LIKE lower(?))`)
				like := "%" + v + "%"
				args = append(args, like, like)
			}
		case "series":
			if f.Exact {
				where.WriteString(` AND lower(coalesce(s.title, '')) = lower(?)`)
				args = append(args, v)
			} else {
				where.WriteString(` AND lower(coalesce(s.title, '')) LIKE lower(?)`)
				args = append(args, "%"+v+"%")
			}
		case "tags", "tag", "genre", "genres":
			if f.Exact {
				where.WriteString(` AND b.id IN (
					SELECT bg.book_id FROM book_genres bg
					JOIN genres g ON g.id = bg.genre_id
					WHERE lower(g.code) = lower(?) OR lower(g.name) = lower(?))`)
				args = append(args, v, v)
			} else {
				where.WriteString(` AND b.id IN (
					SELECT bg.book_id FROM book_genres bg
					JOIN genres g ON g.id = bg.genre_id
					WHERE lower(g.code) LIKE lower(?) OR lower(g.name) LIKE lower(?))`)
				like := "%" + v + "%"
				args = append(args, like, like)
			}
		case "languages", "language", "lang":
			code := NormalizeLang(v)
			if code == "" {
				code = strings.ToLower(v)
			}
			if code == LangUnknown {
				where.WriteString(` AND b.lang = ''`)
			} else {
				where.WriteString(` AND lower(b.lang) = lower(?)`)
				args = append(args, code)
			}
		case "formats", "format", "ext":
			ext := strings.TrimPrefix(strings.ToLower(v), ".")
			if f.Exact {
				where.WriteString(` AND lower(b.ext) = ?`)
				args = append(args, ext)
			} else {
				where.WriteString(` AND lower(b.ext) LIKE ?`)
				args = append(args, "%"+ext+"%")
			}
		case "publisher":
			// Polka has no publisher index; ignore gracefully.
			continue
		default:
			return "", nil, fmt.Errorf("unknown search field %q", f.Field)
		}
	}
	return where.String(), args, nil
}
