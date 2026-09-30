// Package enrich enriches books with external ratings and covers:
// LiveLib, Google Books, Open Library. Results are cached on disk.
package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SourceLiveLib     = "livelib"
	SourceGoogleBooks = "google_books"
	SourceOpenLibrary = "open_library"

	successTTL   = 30 * 24 * time.Hour
	negativeTTL  = 7 * 24 * time.Hour
	transientTTL = 5 * time.Minute
	httpTimeout  = 6 * time.Second
)

// Sources lists all sources in priority order.
var Sources = []string{SourceLiveLib, SourceGoogleBooks, SourceOpenLibrary}

type Result struct {
	Source       string  `json:"source"`
	Rating       float64 `json:"rating"`
	RatingsCount int     `json:"ratingsCount,omitempty"`
	InfoURL      string  `json:"infoUrl,omitempty"`
	CoverURL     string  `json:"coverUrl,omitempty"`
}

type Enrichment struct {
	Primary          *Result  `json:"primary"`
	Sources          []Result `json:"sources"`
	Negative         bool     `json:"negative"`
	ExtraSourceCount int      `json:"extraSourceCount"`
	CoverURL         string   `json:"coverUrl,omitempty"`
}

type cacheEntry struct {
	Result    *Enrichment `json:"result"`
	FetchedAt time.Time   `json:"fetchedAt"`
}

type Provider struct {
	client    *http.Client
	cachePath string

	mu      sync.Mutex
	cache   map[string]cacheEntry
	similar map[string]similarEntry
	reviews map[string]reviewsEntry
	backoff map[string]time.Time // source -> do not query until

	reviewsWait map[string]chan struct{} // in-flight review fetches by cache key
	enrichWait  map[string]chan struct{}
	similarWait map[string]chan struct{}

	// Base URLs are overridden in tests.
	LiveLibBase     string
	GoogleBase      string
	OpenLibraryBase string
	FantLabBase     string
	TasteDiveBase   string
	HardcoverBase   string
	NYTBase         string
}

func New(cachePath string) *Provider {
	p := &Provider{
		client:          &http.Client{Timeout: httpTimeout},
		cachePath:       cachePath,
		cache:           map[string]cacheEntry{},
		backoff:         map[string]time.Time{},
		LiveLibBase:     "https://www.livelib.ru",
		GoogleBase:      "https://www.googleapis.com",
		OpenLibraryBase: "https://openlibrary.org",
		FantLabBase:     "https://api.fantlab.ru",
		TasteDiveBase:   "https://tastedive.com",
		HardcoverBase:   "https://api.hardcover.app",
		NYTBase:         "https://api.nytimes.com",
	}
	p.loadCache()
	return p
}

func (p *Provider) loadCache() {
	data, err := os.ReadFile(p.cachePath)
	if err != nil {
		return
	}
	json.Unmarshal(data, &p.cache)
}

func (p *Provider) persistCache() {
	data, err := json.Marshal(p.cache)
	if err != nil {
		return
	}
	tmp := p.cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	os.Rename(tmp, p.cachePath)
}

// GetCached returns a fresh enrichment from disk/memory cache without network calls.
func (p *Provider) GetCached(key string) (*Enrichment, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.cache[key]
	if !ok {
		return nil, false
	}
	ttl := successTTL
	if e.Result != nil && e.Result.Negative {
		ttl = negativeTTL
	}
	if time.Since(e.FetchedAt) >= ttl {
		return nil, false
	}
	return e.Result, true
}

// WarmGet fills enrichment in the background.
func (p *Provider) WarmGet(key, title, author string, enabled map[string]bool) {
	if _, ok := p.GetCached(key); ok {
		return
	}
	p.mu.Lock()
	if p.enrichWait == nil {
		p.enrichWait = map[string]chan struct{}{}
	}
	if _, inflight := p.enrichWait[key]; inflight {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		p.Get(ctx, key, title, author, enabled)
	}()
}

// Get returns book enrichment, querying the enabled sources.
func (p *Provider) Get(ctx context.Context, key, title, author string, enabled map[string]bool) *Enrichment {
	if cached, ok := p.GetCached(key); ok {
		return cached
	}

	p.mu.Lock()
	if p.enrichWait == nil {
		p.enrichWait = map[string]chan struct{}{}
	}
	if wait, ok := p.enrichWait[key]; ok {
		p.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return &Enrichment{Negative: true}
		}
		if cached, ok := p.GetCached(key); ok {
			return cached
		}
		return &Enrichment{Negative: true}
	}
	done := make(chan struct{})
	p.enrichWait[key] = done
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.enrichWait, key)
		close(done)
		p.mu.Unlock()
	}()

	type srcOut struct {
		source string
		r      *Result
		err    error
	}
	var jobs []string
	for _, source := range Sources {
		if !enabled[source] {
			continue
		}
		p.mu.Lock()
		waitUntil := p.backoff[source]
		p.mu.Unlock()
		if time.Now().Before(waitUntil) {
			continue
		}
		jobs = append(jobs, source)
	}

	outs := make([]srcOut, len(jobs))
	var wg sync.WaitGroup
	for i, source := range jobs {
		wg.Add(1)
		go func(i int, source string) {
			defer wg.Done()
			r, err := p.fetch(ctx, source, title, author)
			outs[i] = srcOut{source: source, r: r, err: err}
		}(i, source)
	}
	wg.Wait()

	res := &Enrichment{}
	transientOnly := true
	// Keep Sources order for primary priority.
	bySource := map[string]srcOut{}
	for _, o := range outs {
		bySource[o.source] = o
	}
	for _, source := range Sources {
		o, ok := bySource[source]
		if !ok {
			continue
		}
		if o.err != nil {
			p.mu.Lock()
			p.backoff[source] = time.Now().Add(transientTTL)
			p.mu.Unlock()
			continue
		}
		transientOnly = false
		if o.r != nil {
			res.Sources = append(res.Sources, *o.r)
			if res.Primary == nil {
				res.Primary = o.r
			}
			if res.CoverURL == "" {
				res.CoverURL = o.r.CoverURL
			}
		}
	}

	if len(res.Sources) > 0 {
		res.ExtraSourceCount = len(res.Sources) - 1
	} else {
		res.Negative = true
		if transientOnly {
			// No source responded — do not remember it as "not found".
			return res
		}
	}

	p.mu.Lock()
	p.cache[key] = cacheEntry{Result: res, FetchedAt: time.Now()}
	p.persistCache()
	p.mu.Unlock()
	return res
}

func (p *Provider) fetch(ctx context.Context, source, title, author string) (*Result, error) {
	switch source {
	case SourceLiveLib:
		return p.fetchLiveLib(ctx, title, author)
	case SourceGoogleBooks:
		return p.fetchGoogleBooks(ctx, title, author)
	case SourceOpenLibrary:
		return p.fetchOpenLibrary(ctx, title, author)
	}
	return nil, nil
}

func (p *Provider) get(ctx context.Context, rawURL string, browser bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if browser {
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101 Firefox/125.0")
		req.Header.Set("Accept-Language", "ru,en;q=0.7")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", rawURL, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// --- LiveLib: search page parsing ---

var livelibRe = regexp.MustCompile(`(?s)href="(/book/\d+[^"]*)".*?itemprop="ratingValue">([\d.,]+)`)

func (p *Provider) fetchLiveLib(ctx context.Context, title, author string) (*Result, error) {
	q := strings.TrimSpace(title)
	body, err := p.get(ctx, p.LiveLibBase+"/find/books/"+url.PathEscape(q), true)
	if err != nil {
		return nil, err
	}
	m := livelibRe.FindSubmatch(body)
	if m == nil {
		return nil, nil // page fetched, no book found
	}
	rating, err := strconv.ParseFloat(strings.ReplaceAll(string(m[2]), ",", "."), 64)
	if err != nil || rating <= 0 {
		return nil, nil
	}
	return &Result{
		Source:  SourceLiveLib,
		Rating:  rating,
		InfoURL: p.LiveLibBase + string(m[1]),
	}, nil
}

// --- Google Books ---

func (p *Provider) fetchGoogleBooks(ctx context.Context, title, author string) (*Result, error) {
	q := fmt.Sprintf(`intitle:%q`, title)
	if author != "" {
		q += fmt.Sprintf(` inauthor:%q`, author)
	}
	u := p.GoogleBase + "/books/v1/volumes?maxResults=10&q=" + url.QueryEscape(q)
	body, err := p.get(ctx, u, false)
	if err != nil {
		return nil, err
	}
	var data struct {
		Items []struct {
			VolumeInfo struct {
				AverageRating float64 `json:"averageRating"`
				RatingsCount  int     `json:"ratingsCount"`
				InfoLink      string  `json:"infoLink"`
				ImageLinks    struct {
					Thumbnail string `json:"thumbnail"`
				} `json:"imageLinks"`
			} `json:"volumeInfo"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	for _, item := range data.Items {
		v := item.VolumeInfo
		if v.AverageRating <= 0 {
			continue
		}
		return &Result{
			Source:       SourceGoogleBooks,
			Rating:       v.AverageRating,
			RatingsCount: v.RatingsCount,
			InfoURL:      v.InfoLink,
			CoverURL:     strings.Replace(v.ImageLinks.Thumbnail, "http://", "https://", 1),
		}, nil
	}
	return nil, nil
}

// --- Open Library ---

func (p *Provider) fetchOpenLibrary(ctx context.Context, title, author string) (*Result, error) {
	params := url.Values{
		"title":  {title},
		"limit":  {"5"},
		"fields": {"key,title,ratings_average,ratings_count,cover_i"},
	}
	if author != "" {
		params.Set("author", author)
	}
	body, err := p.get(ctx, p.OpenLibraryBase+"/search.json?"+params.Encode(), false)
	if err != nil {
		return nil, err
	}
	var data struct {
		Docs []struct {
			Key            string  `json:"key"`
			RatingsAverage float64 `json:"ratings_average"`
			RatingsCount   int     `json:"ratings_count"`
			CoverI         int     `json:"cover_i"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	for _, doc := range data.Docs {
		if doc.RatingsAverage <= 0 {
			continue
		}
		r := &Result{
			Source:       SourceOpenLibrary,
			Rating:       doc.RatingsAverage,
			RatingsCount: doc.RatingsCount,
			InfoURL:      p.OpenLibraryBase + doc.Key,
		}
		if doc.CoverI > 0 {
			r.CoverURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", doc.CoverI)
		}
		return r, nil
	}
	return nil, nil
}
