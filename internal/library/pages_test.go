package library

import (
	"strings"
	"testing"
)

func TestEstimatePages(t *testing.T) {
	if EstimatePages(nil) != 0 {
		t.Fatal("nil")
	}
	short := &FB2Text{Chapters: []FB2Chapter{{HTML: "<p>Короткий текст.</p>"}}}
	if got := EstimatePages(short); got != 1 {
		t.Fatalf("short = %d, want 1", got)
	}
	var long strings.Builder
	for i := 0; i < 400; i++ {
		long.WriteString("<p>Слово ещё слово и ещё немного текста для оценки.</p>")
	}
	text := &FB2Text{Chapters: []FB2Chapter{{HTML: long.String()}}}
	if got := EstimatePages(text); got < 2 {
		t.Fatalf("long = %d, want >= 2", got)
	}
}

func TestPlainTextLen(t *testing.T) {
	n := plainTextLen(`<p>Hello&nbsp;<b>world</b></p>`)
	if n != len([]rune("Hello world")) {
		t.Fatalf("plainTextLen = %d", n)
	}
}
