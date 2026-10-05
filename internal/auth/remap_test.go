package auth

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRemapBookIDs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "reader", "password123", "Reader", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProgress(ctx, u.ID, 10, Progress{Chapter: 2, Position: 0.4, Overall: 0.4}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProgress(ctx, u.ID, 20, Progress{Overall: 1.0}); err != nil {
		t.Fatal(err)
	}
	if err := s.RateBook(ctx, u.ID, 10, 5); err != nil {
		t.Fatal(err)
	}
	list, err := s.Wishlist(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddToList(ctx, u.ID, list.ID, 10); err != nil {
		t.Fatal(err)
	}

	n, err := s.RemapBookIDs(ctx, map[int64]int64{10: 100, 20: 200})
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("updated rows = %d", n)
	}

	p, err := s.GetProgress(ctx, u.ID, 100)
	if err != nil || p.Overall != 0.4 {
		t.Fatalf("progress@100 = %+v err=%v", p, err)
	}
	if _, err := s.GetProgress(ctx, u.ID, 10); err == nil {
		t.Fatal("old book id should be gone")
	}
	finished, err := s.FinishedBookIDs(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !finished[200] || finished[100] {
		t.Fatalf("finished = %#v", finished)
	}
	if r := s.UserRating(ctx, u.ID, 100); r != 5 {
		t.Fatalf("rating = %d", r)
	}
	ids, err := s.ListBookIDs(ctx, u.ID, list.ID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 100 {
		t.Fatalf("list books = %#v", ids)
	}
}

func TestRemapBookIDsConflictKeepsDestination(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "reader", "password123", "", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SaveProgress(ctx, u.ID, 1, Progress{Overall: 0.2})
	_ = s.SaveProgress(ctx, u.ID, 2, Progress{Overall: 0.9})

	if _, err := s.RemapBookIDs(ctx, map[int64]int64{1: 2}); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProgress(ctx, u.ID, 2)
	if err != nil || p.Overall != 0.9 {
		t.Fatalf("kept destination progress = %+v err=%v", p, err)
	}
}

func TestPurgeMismatchedProgressClones(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "reader", "password123", "", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	// Identical fan-out clones (same timestamp).
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO reading_progress (user_id, book_id, chapter, position, overall, locator, updated_at)
		VALUES
			(?, 10, 0, 0, 1, '', '2024-01-01 12:00:00.000'),
			(?, 11, 0, 0, 1, '', '2024-01-01 12:00:00.000')`, u.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	fileOf := map[int64]string{
		10: "a\x00" + "1\x00" + "fb2",
		11: "b\x00" + "2\x00" + "fb2",
	}
	fileCount := map[string]int{
		fileOf[10]: 3, // real series siblings
		fileOf[11]: 1, // unrelated reused rowid
	}
	n, err := s.PurgeMismatchedProgressClones(ctx, fileOf, fileCount)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged = %d", n)
	}
	finished, err := s.FinishedBookIDs(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !finished[10] || finished[11] {
		t.Fatalf("finished = %#v", finished)
	}
}

func TestPurgeBookRefsNotIn(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "reader", "password123", "", RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SaveProgress(ctx, u.ID, 10, Progress{Overall: 1})
	_ = s.SaveProgress(ctx, u.ID, 11, Progress{Overall: 1}) // orphan sibling id
	_ = s.RateBook(ctx, u.ID, 11, 5)

	n, err := s.PurgeBookRefsNotIn(ctx, map[int64]bool{10: true})
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("purged rows = %d", n)
	}
	finished, err := s.FinishedBookIDs(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !finished[10] || finished[11] {
		t.Fatalf("finished after purge = %#v", finished)
	}
	if s.UserRating(ctx, u.ID, 11) != 0 {
		t.Fatal("orphan rating should be gone")
	}
}
