package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSeriesForFileAndSiblings(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sib.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	id1, err := st.AddBook(ctx, &BookInput{
		Title: "Правила крови", Series: "Тайный город", SeriesNum: 10,
		Authors: []AuthorName{{Last: "Панов"}}, Folder: "a.zip", File: "42", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := st.AddBook(ctx, &BookInput{
		Title: "Правила крови", Series: "Правила крови", SeriesNum: 3,
		Authors: []AuthorName{{Last: "Панов"}}, Folder: "a.zip", File: "42", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Different file — must not mix in.
	if _, err := st.AddBook(ctx, &BookInput{
		Title: "Правила крови", Series: "Другая", Authors: []AuthorName{{Last: "Ренделл"}},
		Folder: "a.zip", File: "99", Ext: "fb2", Lang: "ru",
	}); err != nil {
		t.Fatal(err)
	}

	sibs, err := st.BookSiblingIDs(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sibs) != 2 || sibs[0] != id1 || sibs[1] != id2 {
		t.Fatalf("siblings = %v, want [%d %d]", sibs, id1, id2)
	}

	series, err := st.SeriesForFile(ctx, id2)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("series = %+v, want 2", series)
	}
	titles := map[string]int{}
	for _, s := range series {
		titles[s.Title] = s.SeqNumber
	}
	if titles["Тайный город"] != 10 || titles["Правила крови"] != 3 {
		t.Fatalf("series map = %+v", titles)
	}

	d, err := st.BookDetails(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Series) != 2 {
		t.Fatalf("BookDetails.Series = %+v", d.Series)
	}

	expanded, err := st.ExpandSiblingIDs(ctx, map[int64]bool{id1: true})
	if err != nil {
		t.Fatal(err)
	}
	if !expanded[id1] || !expanded[id2] || len(expanded) != 2 {
		t.Fatalf("expand = %#v", expanded)
	}
}
