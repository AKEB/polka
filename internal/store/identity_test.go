package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRemapBookIDsByFileKey(t *testing.T) {
	old := IdentityMaps{
		ByFile: map[string]int64{
			FileKey("a.zip", "1", "fb2"): 10,
			FileKey("a.zip", "2", "fb2"): 20,
			FileKey("b.zip", "3", "fb2"): 30,
		},
		ByLib: map[string]int64{"L1": 10, "L2": 20},
		LibOf: map[int64]string{10: "L1", 20: "L2"},
	}
	neu := IdentityMaps{
		ByFile: map[string]int64{
			FileKey("a.zip", "1", "fb2"): 100, // reordered ids
			FileKey("a.zip", "2", "fb2"): 200,
			FileKey("c.zip", "9", "fb2"): 300, // new book; old 30 gone
		},
		ByLib: map[string]int64{"L1": 100, "L2": 200},
		LibOf: map[int64]string{100: "L1", 200: "L2"},
	}
	m := RemapBookIDs(old, neu)
	if m[10] != 100 || m[20] != 200 {
		t.Fatalf("remap = %#v", m)
	}
	if _, ok := m[30]; ok {
		t.Fatalf("deleted book should not remap: %#v", m)
	}
}

func TestRemapFallsBackToLibID(t *testing.T) {
	old := IdentityMaps{
		ByFile: map[string]int64{FileKey("old.zip", "1", "fb2"): 5},
		ByLib:  map[string]int64{"LIB42": 5},
		LibOf:  map[int64]string{5: "LIB42"},
	}
	neu := IdentityMaps{
		ByFile: map[string]int64{FileKey("new.zip", "1", "fb2"): 77}, // folder renamed
		ByLib:  map[string]int64{"LIB42": 77},
		LibOf:  map[int64]string{77: "LIB42"},
	}
	m := RemapBookIDs(old, neu)
	if m[5] != 77 {
		t.Fatalf("lib_id fallback remap = %#v", m)
	}
}

func TestBookEditsSurviveClear(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "edits.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	id, err := st.AddBook(ctx, &BookInput{
		LibID: "L9", Title: "Старое", Authors: []AuthorName{{Last: "Автор", First: "А"}},
		Folder: "x.zip", File: "9", Ext: "fb2", Lang: "ru", Added: "2024-01-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateBook(ctx, id, BookUpdate{
		Title: "Исправленное", Authors: []AuthorName{{Last: "Автор", First: "Александр"}},
		Lang: "ru", Genres: []string{"sf"},
	}); err != nil {
		t.Fatal(err)
	}

	old, err := st.BookIdentityMaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Insert another book first so the edited book gets a new rowid.
	if err := session.Add(&BookInput{
		LibID: "L0", Title: "Другая", Authors: []AuthorName{{Last: "X"}},
		Folder: "y.zip", File: "1", Ext: "fb2", Lang: "ru", Added: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Add(&BookInput{
		LibID: "L9", Title: "Старое", Authors: []AuthorName{{Last: "Автор", First: "А"}},
		Folder: "x.zip", File: "9", Ext: "fb2", Lang: "ru", Added: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	n, err := st.ApplyBookEdits(ctx)
	if err != nil || n != 1 {
		t.Fatalf("ApplyBookEdits: n=%d err=%v", n, err)
	}
	neu, _ := st.BookIdentityMaps(ctx)
	mapping := RemapBookIDs(old, neu)
	if mapping[id] == 0 || mapping[id] == id {
		t.Fatalf("expected remapped id for %d, got %#v (neu=%#v)", id, mapping, neu.ByFile)
	}

	d, err := st.BookDetails(ctx, neu.ByFile[FileKey("x.zip", "9", "fb2")])
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Исправленное" {
		t.Fatalf("title after reimport = %q", d.Title)
	}
	if len(d.Authors) != 1 || d.Authors[0].First != "Александр" {
		t.Fatalf("authors = %+v", d.Authors)
	}
}
