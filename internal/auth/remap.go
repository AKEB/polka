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
