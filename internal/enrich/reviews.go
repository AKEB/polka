package enrich

import (
	"context"
	"encoding/json"
	"html"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SourceFantLabReviews   = "fantlab"
	SourceLiveLibReviews   = "livelib"
	SourceHardcoverReviews = "hardcover"
	SourceNYTReviews       = "nyt"

	reviewsPerSource = 3
	reviewsTotalCap  = 8
	reviewTextLimit  = 400
)

// Review is one external reader/editorial review snippet.
type Review struct {
	Source    string  `json:"source"`
	Author    string  `json:"author,omitempty"`
	Rating    float64 `json:"rating,omitempty"`    // 0 = unknown
	MaxRating float64 `json:"maxRating,omitempty"` // 5 or 10
	Text      string  `json:"text"`
	URL       string  `json:"url,omitempty"`
	Date      string  `json:"date,omitempty"`
}

// ReviewsConfig toggles review sources and holds API credentials.
type ReviewsConfig struct {
	FantLab        bool
	LiveLib        bool
	Hardcover      bool
	HardcoverToken string
	NYT            bool
	NYTKey         string
}

type reviewsEntry struct {
	Results   []Review  `json:"results"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// Reviews returns external reviews for a book (cached on disk).
func (p *Provider) Reviews(ctx context.Context, key, title, author string, cfg ReviewsConfig) []Review {
	p.mu.Lock()
	if p.reviews == nil {
		p.loadReviewsCache()
	}
	if e, ok := p.reviews[key]; ok && time.Since(e.FetchedAt) < successTTL {
		out := e.Results
		p.mu.Unlock()
		return out
	}
	p.mu.Unlock()

	var out []Review
	transient := false

	try := func(source string, enabled bool, fetch func() ([]Review, error)) {
		if !enabled || len(out) >= reviewsTotalCap {
			return
		}
		p.mu.Lock()
		wait := p.backoff["reviews:"+source]
		p.mu.Unlock()
		if time.Now().Before(wait) {
			return
		}
		res, err := fetch()
		if err != nil {
			transient = true
			p.mu.Lock()
			p.backoff["reviews:"+source] = time.Now().Add(transientTTL)
			p.mu.Unlock()
			p.log("reviews "+source, err)
			return
		}
		for _, r := range res {
			if len(out) >= reviewsTotalCap {
				break
			}
			out = append(out, r)
		}
	}

	try(SourceFantLabReviews, cfg.FantLab, func() ([]Review, error) {
		return p.fantlabReviews(ctx, title, author)
	})
	try(SourceLiveLibReviews, cfg.LiveLib, func() ([]Review, error) {
		return p.livelibReviews(ctx, title, author)
	})
	try(SourceHardcoverReviews, cfg.Hardcover && cfg.HardcoverToken != "", func() ([]Review, error) {
		return p.hardcoverReviews(ctx, title, author, cfg.HardcoverToken)
	})
	try(SourceNYTReviews, cfg.NYT && cfg.NYTKey != "", func() ([]Review, error) {
		return p.nytReviews(ctx, title, author, cfg.NYTKey)
	})

	if transient && len(out) == 0 {
		return out
	}
	p.mu.Lock()
	p.reviews[key] = reviewsEntry{Results: out, FetchedAt: time.Now()}
	p.persistReviewsCache()
	p.mu.Unlock()
	return out
}

func (p *Provider) reviewsCachePath() string { return p.cachePath + ".reviews" }

func (p *Provider) loadReviewsCache() {
	p.reviews = map[string]reviewsEntry{}
	data, err := os.ReadFile(p.reviewsCachePath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &p.reviews)
}

func (p *Provider) persistReviewsCache() {
	data, err := json.Marshal(p.reviews)
	if err != nil {
		return
	}
	tmp := p.reviewsCachePath() + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, p.reviewsCachePath())
	}
}

var (
	htmlTagRe      = regexp.MustCompile(`(?is)<[^>]+>`)
	htmlEntitySpace = regexp.MustCompile(`(?i)&nbsp;|&#160;`)
	wsCollapseRe   = regexp.MustCompile(`\s+`)
)

func stripHTML(s string) string {
	s = htmlEntitySpace.ReplaceAllString(s, " ")
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = wsCollapseRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func truncateReview(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	cut := runes[:limit]
	// Prefer breaking on a space near the end.
	if i := strings.LastIndex(string(cut), " "); i > limit*2/3 {
		cut = []rune(string(cut)[:i])
	}
	return strings.TrimSpace(string(cut)) + "…"
}

func normalizeReview(r Review) Review {
	r.Author = strings.TrimSpace(r.Author)
	r.Text = truncateReview(stripHTML(r.Text), reviewTextLimit)
	r.URL = strings.TrimSpace(r.URL)
	r.Date = strings.TrimSpace(r.Date)
	if r.MaxRating == 0 && r.Rating > 0 {
		r.MaxRating = 5
	}
	return r
}
