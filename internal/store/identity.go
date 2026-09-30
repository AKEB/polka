package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// FileKey builds a stable identity for a catalog book across re-imports:
// folder + file + ext (lowercased). Flibusta monthly dumps keep these
// values for existing books even when SQLite rowids are reassigned.
func FileKey(folder, file, ext string) string {
	return strings.ToLower(strings.TrimSpace(folder)) + "\x00" +
		strings.ToLower(strings.TrimSpace(file)) + "\x00" +
		strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}

// IdentityMaps maps stable book identities to current catalog ids.
type IdentityMaps struct {
	ByFile map[string]int64 // FileKey → book id
	ByLib  map[string]int64 // lib_id → book id (only when unique)
	LibOf  map[int64]string // book id → lib_id
}

// BookIdentityMaps snapshots every live book's stable keys.
func (s *Store) BookIdentityMaps(ctx context.Context) (IdentityMaps, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.lib_id, f.name, b.file, b.ext
		FROM books b JOIN folders f ON f.id = b.folder_id
		WHERE b.deleted = 0`)
	if err != nil {
		return IdentityMaps{}, err
	}
	defer rows.Close()

	out := IdentityMaps{
		ByFile: make(map[string]int64, 1024),
		ByLib:  make(map[string]int64, 1024),
		LibOf:  make(map[int64]string, 1024),
	}
	libDup := map[string]bool{}
	for rows.Next() {
		var id int64
		var libID, folder, file, ext string
		if err := rows.Scan(&id, &libID, &folder, &file, &ext); err != nil {
			return IdentityMaps{}, err
		}
		out.ByFile[FileKey(folder, file, ext)] = id
		if libID == "" {
			continue
		}
		out.LibOf[id] = libID
		if prev, ok := out.ByLib[libID]; ok && prev != id {
			libDup[libID] = true
			delete(out.ByLib, libID)
			continue
		}
		if !libDup[libID] {
			out.ByLib[libID] = id
		}
	}
	return out, rows.Err()
}

// RemapBookIDs builds oldID → newID using file keys first, then lib_id.
func RemapBookIDs(old, neu IdentityMaps) map[int64]int64 {
	out := make(map[int64]int64, len(old.ByFile))
	matched := make(map[int64]bool, len(old.ByFile))
	for key, oldID := range old.ByFile {
		if newID, ok := neu.ByFile[key]; ok {
			matched[oldID] = true
			if newID != oldID {
				out[oldID] = newID
			}
		}
	}
	for oldID, lib := range old.LibOf {
		if matched[oldID] {
			continue
		}
		if newID, ok := neu.ByLib[lib]; ok && newID != oldID {
			out[oldID] = newID
		}
	}
	return out
}

// BookFileKey returns the FileKey and lib_id for a book.
func (s *Store) BookFileKey(ctx context.Context, bookID int64) (fileKey, libID string, err error) {
	var folder, file, ext string
	err = s.db.QueryRowContext(ctx, `
		SELECT b.lib_id, f.name, b.file, b.ext
		FROM books b JOIN folders f ON f.id = b.folder_id
		WHERE b.id = ?`, bookID).Scan(&libID, &folder, &file, &ext)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return FileKey(folder, file, ext), libID, nil
}

// FindBookIDByFileKey looks up a live book by its stable file key.
func (s *Store) FindBookIDByFileKey(ctx context.Context, fileKey string) (int64, error) {
	parts := strings.Split(fileKey, "\x00")
	if len(parts) != 3 {
		return 0, fmt.Errorf("bad file key")
	}
	folder, file, ext := parts[0], parts[1], parts[2]
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT b.id FROM books b JOIN folders f ON f.id = b.folder_id
		WHERE lower(f.name) = ? AND lower(b.file) = ? AND lower(b.ext) = ? AND b.deleted = 0
		LIMIT 1`, folder, file, ext).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return id, err
}

// FindBookIDByLibID looks up a live book by catalog lib_id.
func (s *Store) FindBookIDByLibID(ctx context.Context, libID string) (int64, error) {
	if libID == "" {
		return 0, ErrNotFound
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM books WHERE lib_id = ? AND deleted = 0 LIMIT 1`, libID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return id, err
}
