package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/vestigiumincaligne/polka/internal/enrich"
	"github.com/vestigiumincaligne/polka/internal/store"
)

// Recommendations: personal shelves on the home page and "similar books"
// on the book card (local scoring + external sources).

// userSeeds gathers the user's signals: liked books (rating >= 4),
// finished reads, lists, currently reading; plus everything familiar — for exclusion.
func (s *Server) userSeeds(r *http.Request, userID int64) (seeds, exclude []int64) {
	ctx := r.Context()
	liked, _ := s.users.RatedBookIDs(ctx, userID, 4)
	listed, _ := s.users.AllListBookIDs(ctx, userID)
	var reading, finishedRecent []int64
	if progress, err := s.users.ListProgress(ctx, userID, 100); err == nil {
		for _, p := range progress {
			reading = append(reading, p.BookID)
		}
	}
	if done, err := s.users.ListFinished(ctx, userID, 100, 0); err == nil {
		for _, p := range done {
			finishedRecent = append(finishedRecent, p.BookID)
		}
	}
	rated, _ := s.users.RatedBookIDs(ctx, userID, 0)
	// Full finished set — ListFinished(100) is only a seed sample and misses
	// older reads (worse with FileKey fan-out, which multiplies progress rows).
	allFinished, _ := s.users.FinishedBookIDs(ctx, userID)

	seen := map[int64]bool{}
	add := func(dst *[]int64, ids []int64, cap int) {
		for _, id := range ids {
			if len(*dst) >= cap {
				return
			}
			if !seen[id] {
				seen[id] = true
				*dst = append(*dst, id)
			}
		}
	}
	// Finished reads are the strongest signal, but leave room for ratings/lists:
	// FileKey fan-out and a large finished library would otherwise fill the cap
	// with books that have no series and starve "Continue series".
	add(&seeds, finishedRecent, 40)
	add(&seeds, liked, 60)
	add(&seeds, listed, 60)
	add(&seeds, reading, 60)

	excludeSeen := map[int64]bool{}
	for id := range allFinished {
		excludeSeen[id] = true
		exclude = append(exclude, id)
	}
	for _, ids := range [][]int64{rated, listed, reading, finishedRecent} {
		for _, id := range ids {
			if !excludeSeen[id] {
				excludeSeen[id] = true
				exclude = append(exclude, id)
			}
		}
	}
	return seeds, exclude
}

// bookFinished reports whether bookID (or a FileKey sibling) is fully read.
func (s *Server) bookFinished(ctx context.Context, bookID int64, finished map[int64]bool) bool {
	if len(finished) == 0 {
		return false
	}
	if finished[bookID] {
		return true
	}
	if s.st == nil {
		return false
	}
	sibs, err := s.st.BookSiblingIDs(ctx, bookID)
	if err != nil {
		return false
	}
	for _, id := range sibs {
		if finished[id] {
			return true
		}
	}
	return false
}

// recShelves builds the personal recommendation shelves.
func (s *Server) recShelves(r *http.Request, userID int64, limit int) []map[string]any {
	seeds, exclude := s.userSeeds(r, userID)
	if len(seeds) == 0 {
		return nil
	}
	lang := reqLang(r)
	finished := s.finishedSet(r)
	var shelves []map[string]any

	// Oversample: excluded finished ids should make SQL skip ahead, but also
	// drop any leftover finished cards (legacy progress without full fan-out).
	if next, err := s.st.SeriesContinuations(r.Context(), seeds, exclude, limit*3); err == nil && len(next) > 0 {
		seenTitle := map[string]bool{}
		seenSeries := map[string]bool{}
		deduped := next[:0]
		for _, b := range next {
			if s.bookFinished(r.Context(), b.ID, finished) {
				continue
			}
			titleKey := strings.ToLower(b.Title)
			seriesKey := strings.ToLower(b.SeriesTitle)
			if seriesKey != "" && seenSeries[seriesKey] {
				continue
			}
			if seenTitle[titleKey] {
				continue
			}
			seenTitle[titleKey] = true
			if seriesKey != "" {
				seenSeries[seriesKey] = true
			}
			deduped = append(deduped, b)
			if limit > 0 && len(deduped) >= limit {
				break
			}
		}
		if len(deduped) > 0 {
			shelves = append(shelves, map[string]any{
				"id": "series_next", "title": tr(lang, "shelf.series_next"), "books": booksJSON(deduped), "hasMore": false,
			})
		}
	}
	if recs, err := s.st.RecommendForUser(r.Context(), seeds, exclude, limit*2); err == nil && len(recs) > 0 {
		kept := recs[:0]
		for _, b := range recs {
			if s.bookFinished(r.Context(), b.ID, finished) {
				continue
			}
			kept = append(kept, b)
			if limit > 0 && len(kept) >= limit {
				break
			}
		}
		if len(kept) > 0 {
			shelves = append(shelves, map[string]any{
				"id": "for_you", "title": tr(lang, "shelf.for_you"), "books": booksJSON(kept), "hasMore": false,
			})
		}
	}
	return shelves
}

// similarConfig reads the similar-books source settings.
func (s *Server) similarConfig(r *http.Request) enrich.SimilarConfig {
	ctx := r.Context()
	return enrich.SimilarConfig{
		FantLab:      s.users.GetSetting(ctx, "enrich.similar_fantlab", "1") == "1",
		TasteDive:    s.users.GetSetting(ctx, "enrich.similar_tastedive", "0") == "1",
		TasteDiveKey: s.users.GetSetting(ctx, "tastedive_key", ""),
	}
}

func (s *Server) reviewsConfig(r *http.Request) enrich.ReviewsConfig {
	ctx := r.Context()
	return enrich.ReviewsConfig{
		FantLab:        s.users.GetSetting(ctx, "enrich.reviews_fantlab", "1") == "1",
		LiveLib:        s.users.GetSetting(ctx, "enrich.reviews_livelib", "1") == "1",
		Hardcover:      s.users.GetSetting(ctx, "enrich.reviews_hardcover", "0") == "1",
		HardcoverToken: s.users.GetSetting(ctx, "hardcover_token", ""),
		NYT:            s.users.GetSetting(ctx, "enrich.reviews_nyt", "0") == "1",
		NYTKey:         s.users.GetSetting(ctx, "nyt_books_key", ""),
	}
}

// GET /main/getBooks/getExternalReviews?bookId&title&author
func (s *Server) handleGetReviews(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	bookID := q.Get("bookId")
	title := strings.TrimSpace(strings.ReplaceAll(q.Get("title"), "+", " "))
	author := strings.TrimSpace(strings.ReplaceAll(q.Get("author"), "+", " "))
	if bookID == "" || title == "" {
		http.Error(w, "bookId and title are required", http.StatusBadRequest)
		return
	}
	key := enrich.ReviewsCacheKey + bookID
	cfg := s.reviewsConfig(r)
	if reviews, ok := s.enrich.ReviewsCached(key); ok {
		writeJSON(w, map[string]any{"reviews": reviews, "pending": false})
		return
	}
	// Do not block the book page on external scrapers — warm in background and poll.
	s.enrich.WarmReviews(key, title, author, cfg)
	writeJSON(w, map[string]any{"reviews": []enrich.Review{}, "pending": true})
}

// GET /main/getBooks/getSimilarBooks?bookId&title&author
func (s *Server) handleGetSimilarBooks(w http.ResponseWriter, r *http.Request) {
	bookID, ok := idParam(r, "bookId")
	if !ok {
		http.Error(w, "bookId required", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(strings.ReplaceAll(r.URL.Query().Get("title"), "+", " "))
	author := strings.TrimSpace(strings.ReplaceAll(r.URL.Query().Get("author"), "+", " "))

	local, err := s.st.SimilarBooks(r.Context(), bookID, 24)
	if err != nil {
		s.apiError(w, err)
		return
	}
	// Diversity: don't let one author take over the whole similar shelf.
	perAuthor := map[string]int{}
	diverse := local[:0]
	for _, b := range local {
		key := strings.ToLower(b.AuthorNames)
		if perAuthor[key] >= 5 {
			continue
		}
		perAuthor[key]++
		diverse = append(diverse, b)
		if len(diverse) >= 12 {
			break
		}
	}
	local = diverse
	seen := map[int64]bool{bookID: true}
	for _, b := range local {
		seen[b.ID] = true
	}

	// External sources: ones matched to the collection become cards,
	// the rest a plain-text list.
	var externalCards []store.Book
	var externalOnly []map[string]any
	if title != "" {
		cfg := s.similarConfig(r)
		simKey := "sim:" + strconv.FormatInt(bookID, 10)
		for _, sim := range s.enrich.Similar(r.Context(), simKey, title, author, cfg) {
			if b, err := s.st.MatchBook(r.Context(), sim.Title, sim.Author); err == nil {
				if !seen[b.ID] {
					seen[b.ID] = true
					externalCards = append(externalCards, *b)
				}
				continue
			}
			externalOnly = append(externalOnly, map[string]any{
				"title": sim.Title, "author": sim.Author, "source": sim.Source,
			})
		}
	}

	books := booksJSON(local)
	books = append(books, booksJSON(externalCards)...)
	if len(books) > 18 {
		books = books[:18]
	}
	s.markFinished(r, books)
	writeJSON(w, map[string]any{"similar": books, "external": externalOnly})
}
