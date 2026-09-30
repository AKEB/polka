package enrich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// fantlabFindWorkID searches FantLab and returns a work id, preferring an author match.
func (p *Provider) fantlabFindWorkID(ctx context.Context, title, author string) (string, error) {
	body, err := p.get(ctx, p.FantLabBase+"/search-works?q="+url.QueryEscape(title), false)
	if err != nil {
		return "", err
	}
	var search struct {
		Matches []struct {
			WorkID   json.Number `json:"work_id"`
			AutorRus string      `json:"autor_rusname"`
			AllRus   string      `json:"all_autor_rusname"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(body, &search); err != nil {
		return "", err
	}
	if len(search.Matches) == 0 {
		return "", nil
	}
	pick := search.Matches[0]
	if author != "" {
		last := strings.Fields(author)[0]
		for _, m := range search.Matches {
			if strings.Contains(strings.ToLower(m.AllRus+m.AutorRus), strings.ToLower(last)) {
				pick = m
				break
			}
		}
	}
	id := pick.WorkID.String()
	if id == "" || id == "0" {
		return "", nil
	}
	return id, nil
}

func (p *Provider) fantlabReviews(ctx context.Context, title, author string) ([]Review, error) {
	workID, err := p.fantlabFindWorkID(ctx, title, author)
	if err != nil || workID == "" {
		return nil, err
	}
	body, err := p.get(ctx, p.FantLabBase+"/work/"+workID+"/responses?sort=rating", false)
	if err != nil {
		return nil, err
	}
	var data struct {
		Items []struct {
			ResponseID   int    `json:"response_id"`
			ResponseText string `json:"response_text"`
			Mark         int    `json:"mark"`
			Date         string `json:"response_date"`
			UserName     string `json:"user_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	var out []Review
	for _, item := range data.Items {
		text := stripHTML(item.ResponseText)
		if text == "" {
			continue
		}
		r := Review{
			Source:    SourceFantLabReviews,
			Author:    item.UserName,
			Text:      text,
			Date:      item.Date,
			URL:       fmt.Sprintf("https://fantlab.ru/response%d", item.ResponseID),
			MaxRating: 10,
		}
		if item.Mark > 0 {
			r.Rating = float64(item.Mark)
		}
		out = append(out, normalizeReview(r))
		if len(out) >= reviewsPerSource {
			break
		}
	}
	return out, nil
}

// livelibFindBookURL returns the first book page URL from LiveLib search.
func (p *Provider) livelibFindBookURL(ctx context.Context, title string) (string, error) {
	q := strings.TrimSpace(title)
	if q == "" {
		return "", nil
	}
	body, err := p.get(ctx, p.LiveLibBase+"/find/books/"+url.PathEscape(q), true)
	if err != nil {
		return "", err
	}
	m := livelibRe.FindSubmatch(body)
	if m == nil {
		// Fallback: any /book/… link on the search page.
		alt := regexp.MustCompile(`href="(/book/\d+[^"]*)"`).FindSubmatch(body)
		if alt == nil {
			return "", nil
		}
		return p.LiveLibBase + string(alt[1]), nil
	}
	return p.LiveLibBase + string(m[1]), nil
}

var (
	livelibReviewURLRe  = regexp.MustCompile(`(?i)href="(/review/\d+[^"]*)"`)
	livelibReviewUserRe = regexp.MustCompile(`(?is)class="[^"]*(?:user-name|reader-name|ll-review-username)[^"]*"[^>]*>([^<]+)<`)
	livelibReviewTextRe = regexp.MustCompile(`(?is)(?:itemprop="reviewBody"|class="[^"]*(?:review-text|ll-review-text)[^"]*")[^>]*>(.*?)</(?:div|p|span)>`)
	livelibReviewMarkRe = regexp.MustCompile(`(?is)(?:itemprop="ratingValue"|class="[^"]*rating-value[^"]*")[^>]*>\s*([\d.,]+)`)
	livelibReviewCardRe = regexp.MustCompile(`(?is)<(?:article|div)[^>]*(?:data-review-id|lenta-card)[^>]*>[\s\S]*?</(?:article|div)>`)
)

func (p *Provider) livelibReviews(ctx context.Context, title, author string) ([]Review, error) {
	_ = author
	bookURL, err := p.livelibFindBookURL(ctx, title)
	if err != nil || bookURL == "" {
		return nil, err
	}
	body, err := p.get(ctx, bookURL, true)
	if err != nil {
		return nil, err
	}
	return parseLiveLibReviews(body, p.LiveLibBase, bookURL), nil
}

func parseLiveLibReviews(body []byte, base, bookURL string) []Review {
	chunks := livelibReviewCardRe.FindAll(body, 20)
	var out []Review
	seen := map[string]bool{}
	for _, chunk := range chunks {
		if len(chunk) < 80 {
			continue
		}
		urlM := livelibReviewURLRe.FindSubmatch(chunk)
		textM := livelibReviewTextRe.FindSubmatch(chunk)
		text := ""
		if textM != nil {
			text = stripHTML(string(textM[1]))
		}
		if text == "" {
			plain := stripHTML(string(chunk))
			if len([]rune(plain)) < 40 {
				continue
			}
			text = plain
		}
		revURL := bookURL
		if urlM != nil {
			revURL = base + string(urlM[1])
		}
		key := revURL
		if len(text) > 40 {
			key += text[:40]
		} else {
			key += text
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		r := Review{Source: SourceLiveLibReviews, Text: text, URL: revURL, MaxRating: 5}
		if um := livelibReviewUserRe.FindSubmatch(chunk); um != nil {
			r.Author = stripHTML(string(um[1]))
		}
		if mm := livelibReviewMarkRe.FindSubmatch(chunk); mm != nil {
			if v, err := strconv.ParseFloat(strings.ReplaceAll(string(mm[1]), ",", "."), 64); err == nil && v > 0 {
				r.Rating = v
			}
		}
		out = append(out, normalizeReview(r))
		if len(out) >= reviewsPerSource {
			break
		}
	}
	return out
}

func (p *Provider) hardcoverReviews(ctx context.Context, title, author, token string) ([]Review, error) {
	query := `
query BookReviews($title: String!) {
  books(where: {title: {_eq: $title}}, limit: 3) {
    id
    title
    slug
    user_books(where: {has_review: {_eq: true}}, limit: 5, order_by: {rating: desc_nulls_last}) {
      rating
      review_raw
      reviewed_at
      user { username }
    }
  }
}`
	payload, _ := json.Marshal(map[string]any{
		"query":     query,
		"variables": map[string]any{"title": title},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.HardcoverBase+"/v1/graphql", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hardcover: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var data struct {
		Data struct {
			Books []struct {
				ID        int    `json:"id"`
				Slug      string `json:"slug"`
				UserBooks []struct {
					Rating     *float64 `json:"rating"`
					ReviewRaw  string   `json:"review_raw"`
					ReviewedAt string   `json:"reviewed_at"`
					User       struct {
						Username string `json:"username"`
					} `json:"user"`
				} `json:"user_books"`
			} `json:"books"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, fmt.Errorf("hardcover: %s", data.Errors[0].Message)
	}
	// Prefer a book whose contributions/author match when possible; otherwise first with reviews.
	pickIdx := -1
	for i, b := range data.Data.Books {
		if len(b.UserBooks) == 0 {
			continue
		}
		if pickIdx < 0 {
			pickIdx = i
		}
		if author != "" && strings.Contains(strings.ToLower(b.Slug), strings.ToLower(strings.Fields(author)[0])) {
			pickIdx = i
			break
		}
	}
	if pickIdx < 0 {
		return nil, nil
	}
	b := data.Data.Books[pickIdx]
	var out []Review
	for _, ub := range b.UserBooks {
		text := stripHTML(ub.ReviewRaw)
		if text == "" {
			continue
		}
		r := Review{
			Source:    SourceHardcoverReviews,
			Author:    ub.User.Username,
			Text:      text,
			Date:      ub.ReviewedAt,
			URL:       fmt.Sprintf("https://hardcover.app/books/%s", b.Slug),
			MaxRating: 5,
		}
		if ub.Rating != nil && *ub.Rating > 0 {
			r.Rating = *ub.Rating
		}
		out = append(out, normalizeReview(r))
		if len(out) >= reviewsPerSource {
			break
		}
	}
	return out, nil
}

func (p *Provider) nytReviews(ctx context.Context, title, author, apiKey string) ([]Review, error) {
	params := url.Values{"api-key": {apiKey}, "title": {title}}
	if author != "" {
		params.Set("author", author)
	}
	body, err := p.get(ctx, p.NYTBase+"/svc/books/v3/reviews.json?"+params.Encode(), false)
	if err != nil {
		return nil, err
	}
	var data struct {
		Results []struct {
			Byline    string `json:"byline"`
			Summary   string `json:"summary"`
			URL       string `json:"url"`
			BookTitle string `json:"book_title"`
			PubDate   string `json:"publication_dt"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	var out []Review
	for _, item := range data.Results {
		text := strings.TrimSpace(item.Summary)
		if text == "" {
			continue
		}
		out = append(out, normalizeReview(Review{
			Source: SourceNYTReviews,
			Author: strings.TrimPrefix(strings.TrimSpace(item.Byline), "By "),
			Text:   text,
			URL:    item.URL,
			Date:   item.PubDate,
		}))
		if len(out) >= reviewsPerSource {
			break
		}
	}
	return out, nil
}
