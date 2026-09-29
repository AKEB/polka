package server

import (
	"encoding/json"
	"testing"
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
