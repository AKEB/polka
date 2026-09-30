package enrich

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripHTMLAndTruncate(t *testing.T) {
	got := stripHTML(`<p>Hello&nbsp;<b>world</b></p>`)
	if got != "Hello world" {
		t.Fatalf("stripHTML = %q", got)
	}
	long := strings.Repeat("слово ", 100)
	tr := truncateReview(long, 40)
	if !strings.HasSuffix(tr, "…") {
		t.Fatalf("truncate missing ellipsis: %q", tr)
	}
	if len([]rune(tr)) > 41 { // 40 + ellipsis
		t.Fatalf("still too long: %d", len([]rune(tr)))
	}
}

func TestFantLabReviews(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search-works", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"matches": []map[string]any{
				{"work_id": 42, "rusname": "Гиперион", "autor_rusname": "Симмонс", "all_autor_rusname": "Дэн Симмонс"},
			},
		})
	})
	mux.HandleFunc("/work/42/responses", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"response_id": 7, "response_text": "<p>Отличный роман о паломниках.</p>", "mark": 9, "response_date": "2020-01-02", "user_name": "reader1"},
				{"response_id": 8, "response_text": "Слишком затянуто.", "mark": 6, "user_name": "reader2"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New(filepath.Join(t.TempDir(), "c.json"))
	p.FantLabBase = srv.URL
	out, err := p.fantlabReviews(context.Background(), "Гиперион", "Симмонс")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Source != SourceFantLabReviews || out[0].Author != "reader1" || out[0].Rating != 9 || out[0].MaxRating != 10 {
		t.Fatalf("first = %+v", out[0])
	}
	if !strings.Contains(out[0].Text, "Отличный роман") || strings.Contains(out[0].Text, "<p>") {
		t.Fatalf("text = %q", out[0].Text)
	}
	if out[0].URL != "https://fantlab.ru/work42#response7" {
		t.Fatalf("url = %q", out[0].URL)
	}
}

func TestLiveLibParseReviews(t *testing.T) {
	html := []byte(`
<div class="lenta-card" data-review-id="1">
  <a href="/review/123-foo">Читать</a>
  <a class="user-name">Анна</a>
  <span class="rating-value" itemprop="ratingValue">4</span>
  <div class="review-text" itemprop="reviewBody">Очень понравилась книга, советую всем друзьям и знакомым.</div>
</div>
<article class="lenta-card">
  <a href="/review/456-bar">x</a>
  <span class="ll-review-username">Борис</span>
  <div class="ll-review-text">Короткий отзыв на полях книг.</div>
</article>`)
	out := parseLiveLibReviews(html, "https://www.livelib.ru", "https://www.livelib.ru/book/1")
	if len(out) < 1 {
		t.Fatalf("expected reviews, got %d", len(out))
	}
	if out[0].Author != "Анна" || out[0].Rating != 4 {
		t.Fatalf("first = %+v", out[0])
	}
	if !strings.Contains(out[0].URL, "/review/123") {
		t.Fatalf("url = %q", out[0].URL)
	}
}

func TestHardcoverAndNYTReviews(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"books": []map[string]any{{
					"id": 1, "slug": "dune",
					"user_books": []map[string]any{{
						"rating": 5, "review_raw": "A masterpiece of science fiction.",
						"reviewed_at": "2024-01-01", "user": map[string]string{"username": "paul"},
					}},
				}},
			},
		})
	})
	mux.HandleFunc("/svc/books/v3/reviews.json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api-key") != "nytkey" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"byline": "By Jane Critic", "summary": "An ambitious and sweeping novel.",
				"url": "https://nytimes.com/r/1", "publication_dt": "2020-05-01",
			}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New(filepath.Join(t.TempDir(), "c.json"))
	p.HardcoverBase = srv.URL
	p.NYTBase = srv.URL

	hc, err := p.hardcoverReviews(context.Background(), "Dune", "Herbert", "tok")
	if err != nil || len(hc) != 1 || hc[0].Author != "paul" || hc[0].Rating != 5 {
		t.Fatalf("hardcover = %+v err=%v", hc, err)
	}
	nyt, err := p.nytReviews(context.Background(), "Dune", "Herbert", "nytkey")
	if err != nil || len(nyt) != 1 || nyt[0].Author != "Jane Critic" {
		t.Fatalf("nyt = %+v err=%v", nyt, err)
	}
}

func TestReviewsAggregatesAndCaches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search-works", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"matches": []map[string]any{
			{"work_id": 1, "autor_rusname": "A", "all_autor_rusname": "A"},
		}})
	})
	mux.HandleFunc("/work/1/responses", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"response_id": 1, "response_text": "FantLab review text here.", "mark": 8, "user_name": "fl"},
		}})
	})
	mux.HandleFunc("/find/books/", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<a href="/book/99-x"><span itemprop="ratingValue">4</span></a>`))
	})
	mux.HandleFunc("/book/99-x", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<div class="lenta-card"><a href="/review/1">r</a><a class="user-name">LL</a>
			<div class="review-text" itemprop="reviewBody">LiveLib long enough review text for parsing.</div></div>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New(filepath.Join(t.TempDir(), "c.json"))
	p.FantLabBase = srv.URL
	p.LiveLibBase = srv.URL

	cfg := ReviewsConfig{FantLab: true, LiveLib: true}
	out := p.Reviews(context.Background(), "b1", "Title", "Author", cfg)
	if len(out) < 2 {
		t.Fatalf("want >=2 reviews, got %d %#v", len(out), out)
	}
	// Cached second call should not need network (server still up, but verify length stable).
	out2 := p.Reviews(context.Background(), "b1", "Title", "Author", cfg)
	if len(out2) != len(out) {
		t.Fatalf("cache mismatch %d vs %d", len(out2), len(out))
	}
}
