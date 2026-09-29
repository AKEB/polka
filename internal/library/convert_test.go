package library

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestCanConvert(t *testing.T) {
	if !CanConvert("fb2", "epub") || !CanConvert(".EPUB", "fb2") || !CanConvert("txt", "epub") {
		t.Fatal("expected fb2/epub/txt to convert")
	}
	if CanConvert("pdf", "epub") || CanConvert("fb2", "pdf") || CanConvert("mobi", "fb2") {
		t.Fatal("pdf/mobi must not convert")
	}
	if !CanConvert("fb2", "fb2") || !CanConvert("epub", "epub") {
		t.Fatal("identity conversion is allowed")
	}
}

func TestConvertIdentity(t *testing.T) {
	in := []byte("same-bytes")
	out, err := Convert(in, "fb2", "fb2", ConvertInfo{})
	if err != nil || !bytes.Equal(in, out) {
		t.Fatalf("identity: %v %q", err, out)
	}
}

func TestFB2ToEPUB(t *testing.T) {
	out, err := Convert([]byte(textFB2), "fb2", "epub", ConvertInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if mag := string(out[:2]); mag != "PK" {
		t.Fatalf("epub magic %q, want PK", mag)
	}

	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	if zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Errorf("first entry must be uncompressed mimetype, got %s method %d", zr.File[0].Name, zr.File[0].Method)
	}

	meta, err := parseOPFFromEPUB(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Тест" {
		t.Errorf("title = %q", meta.Title)
	}

	text, err := EPUBText(bytes.NewReader(out), int64(len(out)), func(id string) string { return id })
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, ch := range text.Chapters {
		joined += ch.Title + ch.HTML
	}
	for _, want := range []string{"Глава первая", "курсивом", "Вложенный текст", "амперсанд"} {
		if !strings.Contains(joined, want) {
			t.Errorf("epub missing %q in %s", want, joined[:min(len(joined), 400)])
		}
	}

	cover, mime, err := EPUBCover(bytes.NewReader(out), int64(len(out)))
	if err != nil || len(cover) == 0 || !strings.HasPrefix(mime, "image/") {
		t.Errorf("cover: %d %q %v", len(cover), mime, err)
	}
}

func TestEPUBToFB2(t *testing.T) {
	src := epubZip(t, false, true)
	out, err := Convert(src, "epub", "fb2", ConvertInfo{})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := ParseFB2(bytes.NewReader(out), true)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Test Epub" {
		t.Errorf("title = %q", meta.Title)
	}
	if len(meta.Authors) == 0 || meta.Authors[0].String() == "" {
		t.Errorf("authors = %+v", meta.Authors)
	}
	if len(meta.Cover) == 0 {
		t.Error("expected cover from epub")
	}

	text, err := ParseFB2Text(bytes.NewReader(out), func(id string) string { return id })
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, ch := range text.Chapters {
		joined += ch.Title + ch.HTML
	}
	if !strings.Contains(joined, "Hello") || !strings.Contains(joined, "epub") {
		t.Errorf("fb2 body missing text: %s", joined)
	}
}

func TestTXTConvert(t *testing.T) {
	src := []byte("Первый абзац.\n\nВторой абзац.")
	info := ConvertInfo{
		Title:   "Заметки",
		Authors: []PersonName{{Last: "Автор", First: "А."}},
		Lang:    "ru",
	}
	epub, err := Convert(src, "txt", "epub", info)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := parseOPFFromEPUB(bytes.NewReader(epub), int64(len(epub)))
	if err != nil || meta.Title != "Заметки" {
		t.Fatalf("txt epub meta: %+v %v", meta, err)
	}

	fb2, err := Convert(src, "txt", "fb2", info)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseFB2(bytes.NewReader(fb2), false)
	if err != nil || m.Title != "Заметки" || m.Lang != "ru" {
		t.Fatalf("txt fb2 meta: %+v %v", m, err)
	}
	text, err := ParseFB2Text(bytes.NewReader(fb2), nil)
	if err != nil || len(text.Chapters) == 0 || !strings.Contains(text.Chapters[0].HTML, "Первый абзац") {
		t.Fatalf("txt fb2 text: %+v %v", text, err)
	}
}

func TestConvertRoundTripFB2(t *testing.T) {
	epub, err := Convert([]byte(textFB2), "fb2", "epub", ConvertInfo{})
	if err != nil {
		t.Fatal(err)
	}
	fb2, err := Convert(epub, "epub", "fb2", ConvertInfo{})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := ParseFB2(bytes.NewReader(fb2), false)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Тест" {
		t.Errorf("round-trip title = %q", meta.Title)
	}
	text, err := ParseFB2Text(bytes.NewReader(fb2), nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, ch := range text.Chapters {
		joined += ch.HTML
	}
	if !strings.Contains(joined, "курсивом") {
		t.Errorf("round-trip lost emphasis: %s", joined)
	}
}

func TestUnsupportedConvert(t *testing.T) {
	_, err := Convert([]byte("%PDF"), "pdf", "epub", ConvertInfo{})
	if err != ErrUnsupportedConversion {
		t.Errorf("pdf convert: %v", err)
	}
}
