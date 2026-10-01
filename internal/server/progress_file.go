package server

import (
	"context"
	"errors"

	"github.com/vestigiumincaligne/polka/internal/auth"
)

// siblingBookIDs returns FileKey siblings, or just bookID when the catalog
// has no file info (offline / missing row).
func (s *Server) siblingBookIDs(ctx context.Context, bookID int64) []int64 {
	if s.st == nil {
		return []int64{bookID}
	}
	ids, err := s.st.BookSiblingIDs(ctx, bookID)
	if err != nil || len(ids) == 0 {
		return []int64{bookID}
	}
	return ids
}

// saveProgressForFile writes the same progress to every FileKey sibling.
func (s *Server) saveProgressForFile(ctx context.Context, userID, bookID int64, p auth.Progress) error {
	for _, id := range s.siblingBookIDs(ctx, bookID) {
		cp := p
		cp.BookID = id
		if err := s.users.SaveProgress(ctx, userID, id, cp); err != nil {
			return err
		}
	}
	return nil
}

// deleteProgressForFile clears progress on every FileKey sibling.
func (s *Server) deleteProgressForFile(ctx context.Context, userID, bookID int64) error {
	for _, id := range s.siblingBookIDs(ctx, bookID) {
		if err := s.users.DeleteProgress(ctx, userID, id); err != nil {
			return err
		}
	}
	return nil
}

// progressForFile returns the best progress among FileKey siblings.
func (s *Server) progressForFile(ctx context.Context, userID, bookID int64) (auth.Progress, error) {
	var best auth.Progress
	found := false
	for _, id := range s.siblingBookIDs(ctx, bookID) {
		p, err := s.users.GetProgress(ctx, userID, id)
		if errors.Is(err, auth.ErrNotFound) {
			continue
		}
		if err != nil {
			return auth.Progress{}, err
		}
		if !found || p.Overall > best.Overall || (p.Overall == best.Overall && p.Chapter > best.Chapter) {
			best = p
			found = true
		}
	}
	if !found {
		return auth.Progress{BookID: bookID}, auth.ErrNotFound
	}
	best.BookID = bookID
	return best, nil
}
