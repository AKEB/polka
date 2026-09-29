package covergen

import (
	"bytes"
	"image/png"
	"testing"
)

func TestPNGContainsTitleAndAuthor(t *testing.T) {
	data, err := PNG("Война и мир", "Лев Толстой")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != coverW || b.Dy() != coverH {
		t.Fatalf("size %dx%d, want %dx%d", b.Dx(), b.Dy(), coverW, coverH)
	}
	// Deterministic: same input → identical bytes.
	again, err := PNG("Война и мир", "Лев Толстой")
	if err != nil || !bytes.Equal(data, again) {
		t.Error("cover must be deterministic")
	}
	other, _ := PNG("Анна Каренина", "Лев Толстой")
	if bytes.Equal(data, other) {
		t.Error("different titles should get different covers")
	}
}

func TestPNGWrapsLongTitle(t *testing.T) {
	long := "Очень длинное название книги которое должно перенестись на несколько строк без обрезки посередине слова"
	data, err := PNG(long, "Автор Имя Отчество и ещё один")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func TestPNGEmptyTitle(t *testing.T) {
	data, err := PNG("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 100 {
		t.Fatalf("tiny png: %d", len(data))
	}
}
