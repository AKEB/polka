package auth

import (
	"context"
	"fmt"
)

// RemapBookIDs rewrites book_id references after a catalog re-import.
// Progress, ratings and list memberships follow stable folder/file identity
// via the provided old→new map. Conflicting rows (same user already has the
// new id) keep the destination row and drop the source.
func (s *Service) RemapBookIDs(ctx context.Context, mapping map[int64]int64) (int, error) {
	if len(mapping) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	updated := 0
	for oldID, newID := range mapping {
		if oldID == newID {
			continue
		}
		// reading_progress: drop old when destination already exists
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM reading_progress
			WHERE book_id = ? AND user_id IN (
				SELECT user_id FROM (
					SELECT user_id FROM reading_progress WHERE book_id = ?
				)
			)`, oldID, newID); err != nil {
			return updated, fmt.Errorf("progress conflict: %w", err)
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE reading_progress SET book_id = ? WHERE book_id = ?`, newID, oldID)
		if err != nil {
			return updated, fmt.Errorf("progress remap: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			updated += int(n)
		}

		if _, err := tx.ExecContext(ctx, `
			DELETE FROM book_ratings
			WHERE book_id = ? AND user_id IN (
				SELECT user_id FROM (
					SELECT user_id FROM book_ratings WHERE book_id = ?
				)
			)`, oldID, newID); err != nil {
			return updated, fmt.Errorf("ratings conflict: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE book_ratings SET book_id = ? WHERE book_id = ?`, newID, oldID); err != nil {
			return updated, fmt.Errorf("ratings remap: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			DELETE FROM list_books
			WHERE book_id = ? AND list_id IN (
				SELECT list_id FROM (
					SELECT list_id FROM list_books WHERE book_id = ?
				)
			)`, oldID, newID); err != nil {
			return updated, fmt.Errorf("lists conflict: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE list_books SET book_id = ? WHERE book_id = ?`, newID, oldID); err != nil {
			return updated, fmt.Errorf("lists remap: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return updated, err
	}
	return updated, nil
}

// PurgeBookRefsNotIn deletes progress, ratings and list rows whose book_id
// is not in valid. Used after a catalog replace so leftover ids cannot attach
// to unrelated new books that reused SQLite rowids.
func (s *Service) PurgeBookRefsNotIn(ctx context.Context, valid map[int64]bool) (int, error) {
	ids, err := s.referencedBookIDs(ctx)
	if err != nil {
		return 0, err
	}
	var stale []int64
	for _, id := range ids {
		if !valid[id] {
			stale = append(stale, id)
		}
	}
	if len(stale) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	deleted := 0
	for _, id := range stale {
		for _, q := range []string{
			`DELETE FROM reading_progress WHERE book_id = ?`,
			`DELETE FROM book_ratings WHERE book_id = ?`,
			`DELETE FROM list_books WHERE book_id = ?`,
		} {
			res, err := tx.ExecContext(ctx, q, id)
			if err != nil {
				return deleted, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				deleted += int(n)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return deleted, err
	}
	return deleted, nil
}

func (s *Service) referencedBookIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id FROM reading_progress
		UNION
		SELECT book_id FROM book_ratings
		UNION
		SELECT book_id FROM list_books`)
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
