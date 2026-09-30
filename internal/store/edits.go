package store

import (
	"context"
	"encoding/json"
	"errors"
)

// book_edits survives Clear()/re-import. Keys are stable FileKeys so
// admin metadata corrections re-apply onto the new catalog rowids.
const schemaV6 = `
CREATE TABLE IF NOT EXISTS book_edits (
	file_key    TEXT PRIMARY KEY,
	lib_id      TEXT NOT NULL DEFAULT '',
	title       TEXT NOT NULL,
	series      TEXT NOT NULL DEFAULT '',
	series_num  INTEGER NOT NULL DEFAULT 0,
	year        INTEGER NOT NULL DEFAULT 0,
	lang        TEXT NOT NULL DEFAULT '',
	isbn        TEXT NOT NULL DEFAULT '',
	authors_json TEXT NOT NULL DEFAULT '[]',
	genres_json  TEXT NOT NULL DEFAULT '[]',
	updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_book_edits_lib ON book_edits (lib_id) WHERE lib_id != '';
CREATE INDEX IF NOT EXISTS idx_books_lib_id ON books (lib_id) WHERE lib_id != '';
`

type bookEditRow struct {
	FileKey   string
	LibID     string
	Title     string
	Series    string
	SeriesNum int
	Year      int
	Lang      string
	ISBN      string
	Authors   []AuthorName
	Genres    []string
}

func (s *Store) saveBookEdit(ctx context.Context, fileKey, libID string, u BookUpdate) error {
	authorsJSON, err := json.Marshal(u.Authors)
	if err != nil {
		return err
	}
	genresJSON, err := json.Marshal(u.Genres)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO book_edits (file_key, lib_id, title, series, series_num, year, lang, isbn, authors_json, genres_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT (file_key) DO UPDATE SET
			lib_id = excluded.lib_id,
			title = excluded.title,
			series = excluded.series,
			series_num = excluded.series_num,
			year = excluded.year,
			lang = excluded.lang,
			isbn = excluded.isbn,
			authors_json = excluded.authors_json,
			genres_json = excluded.genres_json,
			updated_at = excluded.updated_at`,
		fileKey, libID, u.Title, u.Series, u.SeriesNum, u.Year, u.Lang, NormalizeISBN(u.ISBN),
		string(authorsJSON), string(genresJSON))
	return err
}

func (s *Store) listBookEdits(ctx context.Context) ([]bookEditRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT file_key, lib_id, title, series, series_num, year, lang, isbn, authors_json, genres_json
		FROM book_edits`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bookEditRow
	for rows.Next() {
		var e bookEditRow
		var authorsJSON, genresJSON string
		if err := rows.Scan(&e.FileKey, &e.LibID, &e.Title, &e.Series, &e.SeriesNum,
			&e.Year, &e.Lang, &e.ISBN, &authorsJSON, &genresJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(authorsJSON), &e.Authors)
		_ = json.Unmarshal([]byte(genresJSON), &e.Genres)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ApplyBookEdits re-applies saved metadata corrections onto the current catalog.
// Returns the number of books updated.
func (s *Store) ApplyBookEdits(ctx context.Context) (int, error) {
	edits, err := s.listBookEdits(ctx)
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, e := range edits {
		id, err := s.FindBookIDByFileKey(ctx, e.FileKey)
		if errors.Is(err, ErrNotFound) && e.LibID != "" {
			id, err = s.FindBookIDByLibID(ctx, e.LibID)
		}
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return applied, err
		}
		upd := BookUpdate{
			Title: e.Title, Authors: e.Authors, Series: e.Series, SeriesNum: e.SeriesNum,
			Year: e.Year, Lang: e.Lang, Genres: e.Genres, ISBN: e.ISBN,
		}
		if err := s.updateBookOnly(ctx, id, upd); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

// updateBookOnly applies metadata without rewriting book_edits (used by ApplyBookEdits).
func (s *Store) updateBookOnly(ctx context.Context, bookID int64, u BookUpdate) error {
	return s.updateBook(ctx, bookID, u, false)
}
