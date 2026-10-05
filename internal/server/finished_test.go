package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/store"
)

func TestFinishedToggleAndShelf(t *testing.T) {
	ts, client, _ := newManageServer(t)

	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"a.fb2": []byte(opdsFB2),
	}, nil)
	var up map[string][]uploadResult
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	bookID := up["results"][0].BookID
	id := itoa64(bookID)

	// Put on wishlist first — marking finished should remove it.
	postJSON(t, client, ts.URL+"/api/v1/books/"+id+"/wishlist", map[string]any{"add": true}).Body.Close()

	resp = postJSON(t, client, ts.URL+"/api/v1/books/"+id+"/finished", map[string]any{"done": true})
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != 200 || body["finished"] != true {
		t.Fatalf("mark finished = %d %v", resp.StatusCode, body)
	}

	var shelves map[string][]map[string]any
	resp, _ = client.Get(ts.URL + "/main/getBooks/getHomeShelves")
	json.NewDecoder(resp.Body).Decode(&shelves)
	resp.Body.Close()

	var hasFinished, hasWishlist, hasReading bool
	for _, sh := range shelves["shelves"] {
		switch sh["id"] {
		case "finished":
			hasFinished = true
			if n := len(sh["books"].([]any)); n != 1 {
				t.Errorf("finished shelf books = %d", n)
			}
		case "wishlist":
			hasWishlist = true
		case "reading":
			hasReading = true
		}
	}
	if !hasFinished {
		t.Error("finished shelf missing on home")
	}
	if hasWishlist {
		t.Error("finished book must leave wishlist")
	}
	if hasReading {
		t.Error("finished book must leave reading shelf")
	}

	// Unmark
	resp = postJSON(t, client, ts.URL+"/api/v1/books/"+id+"/finished", map[string]any{"done": false})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("unmark finished -> %d", resp.StatusCode)
	}
	resp, _ = client.Get(ts.URL + "/main/getBooks/getHomeShelves")
	json.NewDecoder(resp.Body).Decode(&shelves)
	resp.Body.Close()
	for _, sh := range shelves["shelves"] {
		if sh["id"] == "finished" {
			t.Error("empty finished shelf must disappear")
		}
	}
}

func TestProgressSharedAcrossFileSiblings(t *testing.T) {
	ts, client, st := newManageServer(t)
	ctx := context.Background()

	id1, err := st.AddBook(ctx, &store.BookInput{
		Title: "Правила крови", Series: "Тайный город", SeriesNum: 10,
		Authors: []store.AuthorName{{Last: "Панов"}}, Folder: "x.zip", File: "7", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := st.AddBook(ctx, &store.BookInput{
		Title: "Правила крови", Series: "Правила крови", SeriesNum: 3,
		Authors: []store.AuthorName{{Last: "Панов"}}, Folder: "x.zip", File: "7", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Partial progress fans out to siblings — reading shelf must show one card.
	resp := postJSON(t, client, ts.URL+"/api/v1/read/"+itoa64(id1)+"/progress", map[string]any{
		"chapter": 2, "position": 0.5, "progress": 0.4,
	})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("progress -> %d", resp.StatusCode)
	}

	var shelves map[string]any
	resp, _ = client.Get(ts.URL + "/main/getBooks/getHomeShelves")
	json.NewDecoder(resp.Body).Decode(&shelves)
	resp.Body.Close()
	var readingBooks int
	for _, sh := range shelves["shelves"].([]any) {
		m := sh.(map[string]any)
		if m["id"] == "reading" {
			readingBooks = len(m["books"].([]any))
		}
	}
	if readingBooks != 1 {
		t.Fatalf("reading shelf should dedupe siblings, got %d", readingBooks)
	}

	// Mark finished on the first sibling.
	resp = postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(id1)+"/finished", map[string]any{"done": true})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("finished -> %d", resp.StatusCode)
	}

	// Progress is visible on the other sibling.
	var prog map[string]any
	resp, _ = client.Get(ts.URL + "/api/v1/read/" + itoa64(id2) + "/progress")
	json.NewDecoder(resp.Body).Decode(&prog)
	resp.Body.Close()
	if prog["progress"].(float64) < 0.98 {
		t.Fatalf("sibling progress = %v", prog)
	}

	// Book form lists both series.
	form := getJSONWith(t, client, ts.URL+"/main/getBooks/getBookForm?selectedItemID="+itoa64(id1))
	series, _ := form["series"].([]any)
	if len(series) != 2 {
		t.Fatalf("series on card = %#v", form["series"])
	}

	// Finished shelf collapses FileKey siblings to one card and exposes see-all.
	resp, _ = client.Get(ts.URL + "/main/getBooks/getHomeShelves")
	json.NewDecoder(resp.Body).Decode(&shelves)
	resp.Body.Close()
	var finishedBooks, stillReading int
	for _, sh := range shelves["shelves"].([]any) {
		m := sh.(map[string]any)
		switch m["id"] {
		case "finished":
			finishedBooks = len(m["books"].([]any))
		case "reading":
			stillReading = len(m["books"].([]any))
		}
	}
	if finishedBooks != 1 {
		t.Fatalf("finished shelf should dedupe siblings, got %d", finishedBooks)
	}
	if stillReading != 0 {
		t.Fatalf("finished book must leave reading shelf, got %d", stillReading)
	}

	page := getJSONWith(t, client, ts.URL+"/main/getBooks/getShelfBooks?shelfId=finished")
	if len(page["titlesList"].([]any)) != 1 {
		t.Fatalf("finished page = %#v", page["titlesList"])
	}
}

func TestSeriesNextSkipsFinishedBeyondRecentSample(t *testing.T) {
	ts, client, st := newManageServer(t)
	ctx := context.Background()

	var series []int64
	for i := 1; i <= 3; i++ {
		id, err := st.AddBook(ctx, &store.BookInput{
			Title: "Цикл-" + itoa64(int64(i)), Series: "Цикл", SeriesNum: i,
			Authors: []store.AuthorName{{Last: "Серийный"}}, Folder: "s.zip", File: "c" + itoa64(int64(i)), Ext: "fb2", Lang: "ru",
		})
		if err != nil {
			t.Fatal(err)
		}
		series = append(series, id)
	}
	// Seed via rating so "Continue series" still sees the cycle after fillers
	// push older finished rows out of ListFinished(100).
	postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(series[0])+"/rating", map[string]int{"rating": 5}).Body.Close()
	postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(series[0])+"/finished", map[string]any{"done": true}).Body.Close()
	postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(series[1])+"/finished", map[string]any{"done": true}).Body.Close()
	for i := 0; i < 120; i++ {
		id, err := st.AddBook(ctx, &store.BookInput{
			Title: "Filler-" + itoa64(int64(i)), Authors: []store.AuthorName{{Last: "X"}},
			Folder: "f.zip", File: "f" + itoa64(int64(i)), Ext: "fb2", Lang: "ru",
		})
		if err != nil {
			t.Fatal(err)
		}
		postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(id)+"/finished", map[string]any{"done": true}).Body.Close()
	}

	next, err := st.SeriesContinuations(ctx, []int64{series[0]}, []int64{series[0], series[1]}, 10)
	if err != nil {
		t.Fatalf("SeriesContinuations: %v", err)
	}
	if len(next) != 1 || next[0].Title != "Цикл-3" {
		t.Fatalf("store continuations = %v", next)
	}

	shelves := getJSONWith(t, client, ts.URL+"/main/getBooks/getHomeShelves")
	var titles []string
	var shelfIDs []string
	for _, sh := range shelves["shelves"].([]any) {
		m := sh.(map[string]any)
		shelfIDs = append(shelfIDs, fmt.Sprint(m["id"]))
		if m["id"] != "series_next" {
			continue
		}
		for _, b := range m["books"].([]any) {
			titles = append(titles, b.(map[string]any)["Title"].(string))
		}
	}
	if len(titles) == 0 {
		t.Fatalf("no series_next books; shelves=%v", shelfIDs)
	}
	for _, title := range titles {
		if title == "Цикл-2" {
			t.Fatalf("finished book must not appear in series_next: %v", titles)
		}
	}
	found3 := false
	for _, title := range titles {
		if title == "Цикл-3" {
			found3 = true
		}
	}
	if !found3 {
		t.Fatalf("series_next should advance to Цикл-3, got %v", titles)
	}
}

func TestReplaceImportDropsOrphanFinishedSiblings(t *testing.T) {
	ts, client, st, dir := newManageServerDir(t)
	ctx := context.Background()
	users, err := auth.Open(filepath.Join(dir, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { users.Close() })

	id1, err := st.AddBook(ctx, &store.BookInput{
		Title: "Книга", Series: "Серия А", SeriesNum: 1, LibID: "L1",
		Authors: []store.AuthorName{{Last: "Автор"}}, Folder: "x.zip", File: "7", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := st.AddBook(ctx, &store.BookInput{
		Title: "Книга", Series: "Серия Б", SeriesNum: 1, LibID: "L2",
		Authors: []store.AuthorName{{Last: "Автор"}}, Folder: "x.zip", File: "7", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(id1)+"/finished", map[string]any{"done": true}).Body.Close()

	admin, err := users.GetByLogin(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// Legacy fan-out: finished row on every sibling id.
	if err := users.SaveProgress(ctx, admin.ID, id2, auth.Progress{Overall: 1}); err != nil {
		t.Fatal(err)
	}

	old, err := st.BookIdentityMaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	// Fresh catalog ids starting over while users.db keeps old progress rows.
	st, err = store.Open(filepath.Join(dir, "catalog2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	other, err := st.AddBook(ctx, &store.BookInput{
		Title: "Чужая", LibID: "OTHER",
		Authors: []store.AuthorName{{Last: "X"}}, Folder: "y.zip", File: "1", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	new1, err := st.AddBook(ctx, &store.BookInput{
		Title: "Книга", Series: "Серия А", SeriesNum: 1, LibID: "L1",
		Authors: []store.AuthorName{{Last: "Автор"}}, Folder: "x.zip", File: "7", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	new2, err := st.AddBook(ctx, &store.BookInput{
		Title: "Книга", Series: "Серия Б", SeriesNum: 1, LibID: "L2",
		Authors: []store.AuthorName{{Last: "Автор"}}, Folder: "x.zip", File: "7", Ext: "fb2", Lang: "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	if other != id1 {
		t.Fatalf("expected rowid reuse for hazard; other=%d id1=%d", other, id1)
	}

	neu, err := st.BookIdentityMaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mapping := store.RemapBookIDs(old, neu)
	if _, err := users.RemapBookIDs(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	if _, err := users.PurgeBookRefsNotIn(ctx, neu.BookIDs()); err != nil {
		t.Fatal(err)
	}

	finished, err := users.FinishedBookIDs(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished[other] {
		t.Fatalf("unrelated book %d marked finished; finished=%#v mapping=%#v", other, finished, mapping)
	}
	if !finished[new1] && !finished[new2] {
		t.Fatalf("real book should stay finished; finished=%#v mapping=%#v", finished, mapping)
	}
}

func TestSeriesNextIgnoresRatingsWithoutReading(t *testing.T) {
	ts, client, st := newManageServer(t)
	ctx := context.Background()

	var series []int64
	for i := 1; i <= 2; i++ {
		id, err := st.AddBook(ctx, &store.BookInput{
			Title: "Том-" + itoa64(int64(i)), Series: "Сага", SeriesNum: i,
			Authors: []store.AuthorName{{Last: "Автор"}}, Folder: "s.zip", File: "t" + itoa64(int64(i)), Ext: "fb2", Lang: "ru",
		})
		if err != nil {
			t.Fatal(err)
		}
		series = append(series, id)
	}
	// High rating alone must not make the series look "started".
	postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(series[0])+"/rating", map[string]int{"rating": 5}).Body.Close()
	postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(series[0])+"/wishlist", map[string]any{"add": true}).Body.Close()

	shelves := getJSONWith(t, client, ts.URL+"/main/getBooks/getHomeShelves")
	for _, sh := range shelves["shelves"].([]any) {
		m := sh.(map[string]any)
		if m["id"] == "series_next" {
			t.Fatalf("series_next must not appear from rating/wishlist alone: %#v", m["books"])
		}
	}
}
