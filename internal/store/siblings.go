package store

import (
	"context"
	"fmt"
	"strings"
)

// BookFileKeys returns FileKey for each live book id in one query.
func (s *Store) BookFileKeys(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	const chunk = 400
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		placeholders := make([]string, len(part))
		args := make([]any, len(part))
		for i, id := range part {
			placeholders[i] = "?"
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT b.id, f.name, b.file, b.ext
			FROM books b JOIN folders f ON f.id = b.folder_id
			WHERE b.id IN (`+strings.Join(placeholders, ",")+`) AND b.deleted = 0`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var folder, file, ext string
			if err := rows.Scan(&id, &folder, &file, &ext); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = FileKey(folder, file, ext)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// BookSiblingIDs returns every live book id that shares the same FileKey
// as bookID (including bookID itself). INPX can list one file under several
// series as separate rows; callers use this to share progress and series.
func (s *Store) BookSiblingIDs(ctx context.Context, bookID int64) ([]int64, error) {
	key, _, err := s.BookFileKey(ctx, bookID)
	if err != nil {
		return nil, err
	}
	return s.BookIDsByFileKey(ctx, key)
}

// BookIDsByFileKey lists live book ids for a FileKey, oldest id first.
func (s *Store) BookIDsByFileKey(ctx context.Context, fileKey string) ([]int64, error) {
	parts := strings.Split(fileKey, "\x00")
	if len(parts) != 3 {
		return nil, fmt.Errorf("bad file key")
	}
	folder, file, ext := parts[0], parts[1], parts[2]
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id FROM books b JOIN folders f ON f.id = b.folder_id
		WHERE lower(f.name) = ? AND lower(b.file) = ? AND lower(b.ext) = ? AND b.deleted = 0
		ORDER BY b.id`, folder, file, ext)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SeriesForFile returns every series assignment for the physical file of bookID.
func (s *Store) SeriesForFile(ctx context.Context, bookID int64) ([]SeriesRef, error) {
	key, _, err := s.BookFileKey(ctx, bookID)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(key, "\x00")
	if len(parts) != 3 {
		return nil, fmt.Errorf("bad file key")
	}
	folder, file, ext := parts[0], parts[1], parts[2]
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.title, coalesce(b.series_num, 0)
		FROM books b
		JOIN folders f ON f.id = b.folder_id
		JOIN series s ON s.id = b.series_id
		WHERE lower(f.name) = ? AND lower(b.file) = ? AND lower(b.ext) = ?
		  AND b.deleted = 0 AND b.series_id IS NOT NULL
		ORDER BY s.title COLLATE NOCASE, coalesce(b.series_num, 0), b.id`, folder, file, ext)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SeriesRef
	seen := map[string]bool{}
	for rows.Next() {
		var r SeriesRef
		if err := rows.Scan(&r.ID, &r.Title, &r.SeqNumber); err != nil {
			return nil, err
		}
		if r.Title == "" {
			continue
		}
		dedupe := strings.ToLower(r.Title) + "\x00" + fmt.Sprint(r.SeqNumber)
		if seen[dedupe] {
			continue
		}
		seen[dedupe] = true
		out = append(out, r)
	}
	return out, rows.Err()
}

// ExpandSiblingIDs adds every FileKey sibling of the given book ids.
func (s *Store) ExpandSiblingIDs(ctx context.Context, ids map[int64]bool) (map[int64]bool, error) {
	if len(ids) == 0 {
		return ids, nil
	}
	list := make([]int64, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	placeholders := make([]string, len(list))
	args := make([]any, len(list))
	for i, id := range list {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `
		SELECT DISTINCT b2.id
		FROM books b
		JOIN books b2 ON b2.folder_id = b.folder_id
			AND lower(b2.file) = lower(b.file)
			AND lower(b2.ext) = lower(b.ext)
		WHERE b.id IN (` + strings.Join(placeholders, ",") + `)
		  AND b.deleted = 0 AND b2.deleted = 0`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Keep originals even if the book was deleted / missing from catalog.
	for id := range ids {
		out[id] = true
	}
	return out, nil
}
