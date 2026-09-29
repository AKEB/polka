package auth

import (
	"context"
	"database/sql"
	"errors"
)

// Reading progress lives in users.db: it is personal and must
// survive collection re-import.

const progressSchema = `
CREATE TABLE IF NOT EXISTS reading_progress (
	user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	book_id    INTEGER NOT NULL,
	chapter    INTEGER NOT NULL DEFAULT 0,
	position   REAL NOT NULL DEFAULT 0,
	overall    REAL NOT NULL DEFAULT 0,
	locator    TEXT NOT NULL DEFAULT '',
	cleared    INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (user_id, book_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_progress_updated ON reading_progress (user_id, updated_at);
`

// migrateProgress upgrades the table from early builds: adds the overall
// column; if it already exists, ALTER fails with duplicate column — ignored.
func migrateProgress(db *sql.DB) {
	db.Exec(`ALTER TABLE reading_progress ADD COLUMN overall REAL NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE reading_progress ADD COLUMN locator TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE reading_progress ADD COLUMN cleared INTEGER NOT NULL DEFAULT 0`)
}

type Progress struct {
	BookID   int64
	Chapter  int
	Position float64 // scroll fraction of the chapter, 0..1
	Overall  float64 // fraction of the whole book, 0..1
	Locator  string  // format-specific position (CFI for epub, etc.)
}

// SaveProgress stores the position with a millisecond timestamp: the
// sync merge is last-write-wins on updated_at, and with whole seconds two
// saves within the same second (e.g. right before and after going
// offline) would compare equal and the later one would be dropped.
func (s *Service) SaveProgress(ctx context.Context, userID, bookID int64, p Progress) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO reading_progress (user_id, book_id, chapter, position, overall, locator, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))
		ON CONFLICT (user_id, book_id) DO UPDATE
		SET chapter = excluded.chapter, position = excluded.position,
		    overall = excluded.overall, locator = excluded.locator,
		    cleared = 0, updated_at = excluded.updated_at`,
		userID, bookID, p.Chapter, p.Position, p.Overall, p.Locator)
	return err
}

func (s *Service) GetProgress(ctx context.Context, userID, bookID int64) (Progress, error) {
	p := Progress{BookID: bookID}
	err := s.db.QueryRowContext(ctx,
		`SELECT chapter, position, overall, locator FROM reading_progress
		 WHERE user_id = ? AND book_id = ? AND cleared = 0`,
		userID, bookID).Scan(&p.Chapter, &p.Position, &p.Overall, &p.Locator)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// ListProgress returns the user's recently read books (newest first).
// Finished books (overall ~1) are not shown.
func (s *Service) ListProgress(ctx context.Context, userID int64, limit int) ([]Progress, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id, chapter, position, overall FROM reading_progress
		WHERE user_id = ? AND cleared = 0 AND overall < 0.98
		ORDER BY updated_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Progress
	for rows.Next() {
		var p Progress
		if err := rows.Scan(&p.BookID, &p.Chapter, &p.Position, &p.Overall); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListFinished returns books the user marked (or reached) as fully read.
func (s *Service) ListFinished(ctx context.Context, userID int64, limit int) ([]Progress, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id, chapter, position, overall FROM reading_progress
		WHERE user_id = ? AND cleared = 0 AND overall >= 0.98
		ORDER BY updated_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Progress
	for rows.Next() {
		var p Progress
		if err := rows.Scan(&p.BookID, &p.Chapter, &p.Position, &p.Overall); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FinishedBookIDs returns the set of fully read book ids for the user.
func (s *Service) FinishedBookIDs(ctx context.Context, userID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id FROM reading_progress
		WHERE user_id = ? AND cleared = 0 AND overall >= 0.98`, userID)
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
	return out, rows.Err()
}

// MarkFinished records the book as fully read (overall = 1).
func (s *Service) MarkFinished(ctx context.Context, userID, bookID int64) error {
	return s.SaveProgress(ctx, userID, bookID, Progress{BookID: bookID, Overall: 1})
}

// IsFinished reports whether the user has fully read the book.
func (s *Service) IsFinished(ctx context.Context, userID, bookID int64) (bool, error) {
	p, err := s.GetProgress(ctx, userID, bookID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return p.Overall >= 0.98, nil
}

// DeleteProgress forgets the reading position so the book leaves
// "Reading now". The row is kept as a tombstone (cleared=1) so a
// desktop client with an older copy cannot resurrect it on sync.
func (s *Service) DeleteProgress(ctx context.Context, userID, bookID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE reading_progress
		SET chapter = 0, position = 0, overall = 0, locator = '', cleared = 1,
		    updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
		WHERE user_id = ? AND book_id = ?`, userID, bookID)
	return err
}
