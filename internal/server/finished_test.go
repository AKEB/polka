package server

import (
	"context"
	"encoding/json"
	"testing"

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

	// Mark finished on the first sibling.
	resp := postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(id1)+"/finished", map[string]any{"done": true})
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
	var shelves map[string]any
	resp, _ = client.Get(ts.URL + "/main/getBooks/getHomeShelves")
	json.NewDecoder(resp.Body).Decode(&shelves)
	resp.Body.Close()
	var finishedBooks int
	for _, sh := range shelves["shelves"].([]any) {
		m := sh.(map[string]any)
		if m["id"] == "finished" {
			finishedBooks = len(m["books"].([]any))
		}
	}
	if finishedBooks != 1 {
		t.Fatalf("finished shelf should dedupe siblings, got %d", finishedBooks)
	}

	page := getJSONWith(t, client, ts.URL+"/main/getBooks/getShelfBooks?shelfId=finished")
	if len(page["titlesList"].([]any)) != 1 {
		t.Fatalf("finished page = %#v", page["titlesList"])
	}
}
