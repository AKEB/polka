package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// newRecsStore: a 3-book series, a favorite author with extra books,
// a high-rated genre, and an unrelated book.
func newRecsStore(t *testing.T) (*Store, map[string]int64) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "recs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	add := func(title, author, series string, num int, genres []string, kw []string, rate float64) {
		b := &BookInput{
			Title:   title,
			Authors: []AuthorName{{Last: author}},
			Series:  series, SeriesNum: num,
			Genres: genres, Keywords: kw,
			Folder: "x.zip", File: title, Ext: "fb2", Lang: "ru", Rate: rate,
			Added: fmt.Sprintf("2024-01-%02d", num+1),
		}
		if err := session.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	add("Цикл-1", "Серийный", "Цикл", 1, []string{"sf"}, []string{"космос"}, 5)
	add("Цикл-2", "Серийный", "Цикл", 2, []string{"sf"}, []string{"космос"}, 5)
	add("Цикл-3", "Серийный", "Цикл", 3, []string{"sf"}, []string{"космос"}, 5)
	add("Другая книга автора", "Серийный", "", 0, []string{"sf"}, nil, 4)
	add("Жанровый хит", "Жанрист", "", 0, []string{"sf"}, []string{"космос"}, 5)
	add("Слабая того же жанра", "Жанрист", "", 0, []string{"sf"}, nil, 1)
	add("Посторонняя", "Чужой", "", 0, []string{"poetry"}, nil, 5)
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}

	ids := map[string]int64{}
	rows, _ := st.DB().Query(`SELECT id, title FROM books`)
	for rows.Next() {
		var id int64
		var title string
		rows.Scan(&id, &title)
		ids[title] = id
	}
	rows.Close()
	return st, ids
}

func titlesOf(books []Book) []string {
	out := make([]string, len(books))
	for i, b := range books {
		out[i] = b.Title
	}
	return out
}

func TestSeriesContinuations(t *testing.T) {
	st, ids := newRecsStore(t)
	ctx := context.Background()

	// Series book 1 has been read → recommend book 2
	next, err := st.SeriesContinuations(ctx, []int64{ids["Цикл-1"]}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || next[0].Title != "Цикл-2" {
		t.Errorf("continuations = %v", titlesOf(next))
	}

	// Series book 2 is excluded (currently being read) → next is book 3
	next, _ = st.SeriesContinuations(ctx, []int64{ids["Цикл-1"]}, []int64{ids["Цикл-2"]}, 10)
	if len(next) != 1 || next[0].Title != "Цикл-3" {
		t.Errorf("with exclude = %v", titlesOf(next))
	}
}

func TestSeriesContinuationsSkipsYearBucketSeries(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "years.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	add := func(title string, num int) {
		if err := session.Add(&BookInput{
			Title: title, Authors: []AuthorName{{Last: "Сб"}}, Series: "Антология поэзии", SeriesNum: num,
			Folder: "y.zip", File: title, Ext: "fb2", Lang: "ru", Rate: 4, Added: "2024-01-01",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("Поэты пражского Скита", 2005)
	add("Век перевода", 2006)
	add("Стихи о маме", 2008)
	// Real numbered series in the same DB must still continue.
	if err := session.Add(&BookInput{
		Title: "Сага-1", Authors: []AuthorName{{Last: "Авт"}}, Series: "Сага", SeriesNum: 1,
		Folder: "s.zip", File: "saga1", Ext: "fb2", Lang: "ru", Rate: 4, Added: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Add(&BookInput{
		Title: "Сага-2", Authors: []AuthorName{{Last: "Авт"}}, Series: "Сага", SeriesNum: 2,
		Folder: "s.zip", File: "saga2", Ext: "fb2", Lang: "ru", Rate: 4, Added: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.DB().Query(`SELECT id, title FROM books`)
	ids := map[string]int64{}
	for rows.Next() {
		var id int64
		var title string
		rows.Scan(&id, &title)
		ids[title] = id
	}
	rows.Close()

	next, err := st.SeriesContinuations(ctx, []int64{ids["Поэты пражского Скита"], ids["Сага-1"]}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := titlesOf(next)
	for _, ttitle := range got {
		if ttitle == "Век перевода" || ttitle == "Стихи о маме" {
			t.Fatalf("year-bucket anthology must not continue: %v", got)
		}
	}
	if len(next) != 1 || next[0].Title != "Сага-2" {
		t.Fatalf("want only Сага-2, got %v", got)
	}
}

func TestSeriesContinuationsOnePerSeries(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "anth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	add := func(title string, num int) {
		if err := session.Add(&BookInput{
			Title: title, Authors: []AuthorName{{Last: "Ант"}}, Series: "Антология", SeriesNum: num,
			Folder: "a.zip", File: title, Ext: "fb2", Lang: "ru", Rate: 4, Added: "2024-01-01",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("Том 0", 1)
	// Same next series_num shared by three anthology entries.
	add("Сборщик душ", 2)
	add("Другие миры", 2)
	add("Историкум", 2)
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.DB().Query(`SELECT id, title FROM books`)
	ids := map[string]int64{}
	for rows.Next() {
		var id int64
		var title string
		rows.Scan(&id, &title)
		ids[title] = id
	}
	rows.Close()

	next, err := st.SeriesContinuations(ctx, []int64{ids["Том 0"]}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 {
		t.Fatalf("want 1 next book per series, got %v", titlesOf(next))
	}
}

func TestRecommendForUser(t *testing.T) {
	st, ids := newRecsStore(t)
	ctx := context.Background()

	seeds := []int64{ids["Цикл-1"]}
	exclude := []int64{ids["Цикл-1"]}
	recs, err := st.RecommendForUser(ctx, seeds, exclude, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, b := range recs {
		got[b.Title] = true
	}
	if !got["Другая книга автора"] {
		t.Errorf("favorite author book missing: %v", titlesOf(recs))
	}
	if !got["Жанровый хит"] {
		t.Errorf("genre hit missing: %v", titlesOf(recs))
	}
	if got["Посторонняя"] || got["Цикл-1"] {
		t.Errorf("unwanted recs: %v", titlesOf(recs))
	}
}

func TestSimilarBooks(t *testing.T) {
	st, ids := newRecsStore(t)
	ctx := context.Background()

	similar, err := st.SimilarBooks(ctx, ids["Цикл-1"], 10)
	if err != nil {
		t.Fatal(err)
	}
	got := titlesOf(similar)
	// The same series is excluded; the genre hit (genre+keyword) must rank above
	// the weak one (genre only, low rating).
	for _, b := range similar {
		if b.SeriesTitle == "Цикл" {
			t.Errorf("same series leaked: %v", got)
		}
	}
	if len(similar) == 0 || similar[0].Title != "Жанровый хит" {
		t.Errorf("similar order = %v", got)
	}
}

func TestMatchBook(t *testing.T) {
	st, _ := newRecsStore(t)
	ctx := context.Background()

	if b, err := st.MatchBook(ctx, "Жанровый хит", "Жанрист"); err != nil || b.Title != "Жанровый хит" {
		t.Errorf("match = %v, %v", b, err)
	}
	// Author mismatch — no match
	if _, err := st.MatchBook(ctx, "Жанровый хит", "Неизвестный"); err == nil {
		t.Error("wrong author must not match")
	}
	if _, err := st.MatchBook(ctx, "Нет такой книги", ""); err == nil {
		t.Error("missing title must not match")
	}
}
