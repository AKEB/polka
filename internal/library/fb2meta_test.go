package library

import (
	"bytes"
	"strings"
	"testing"
)

func TestApplyFB2CatalogMeta(t *testing.T) {
	src := []byte(`<?xml version="1.0" encoding="utf-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
  <description>
    <title-info>
      <author><first-name>Имя</first-name><last-name>Фамилия</last-name></author>
      <book-title>Старое название</book-title>
      <lang>ru</lang>
      <sequence name="Серия" number="1"/>
    </title-info>
  </description>
  <body><section><p>Текст.</p></section></body>
</FictionBook>`)

	out := ApplyFB2CatalogMeta(src, ConvertInfo{
		Title:   "Новое название",
		Authors: []PersonName{{First: "Александр", Last: "Пушкин"}},
		Lang:    "en",
		Series:  "Другая серия",
		SeqNum:  3,
	})
	s := string(out)
	if !strings.Contains(s, "<book-title>Новое название</book-title>") {
		t.Fatalf("title missing: %s", s)
	}
	if strings.Contains(s, "Старое название") {
		t.Fatal("old title still present")
	}
	if !strings.Contains(s, "<last-name>Пушкин</last-name>") || !strings.Contains(s, "<first-name>Александр</first-name>") {
		t.Fatalf("authors: %s", s)
	}
	if !strings.Contains(s, "<lang>en</lang>") {
		t.Fatalf("lang: %s", s)
	}
	if !strings.Contains(s, `name="Другая серия"`) || !strings.Contains(s, `number="3"`) {
		t.Fatalf("series: %s", s)
	}
	if bytes.Contains(out, []byte("Фамилия")) {
		t.Fatal("old author still present")
	}
}

func TestFB2ToEPUBUsesCatalogTitle(t *testing.T) {
	src := []byte(`<?xml version="1.0" encoding="utf-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
  <description><title-info>
    <author><last-name>Old</last-name></author>
    <book-title>File Title</book-title>
    <lang>ru</lang>
  </title-info></description>
  <body><section><p>Hi.</p></section></body>
</FictionBook>`)
	patched := ApplyFB2CatalogMeta(src, ConvertInfo{
		Title:   "Catalog Title",
		Authors: []PersonName{{Last: "CatalogAuthor", First: "A"}},
		Lang:    "ru",
	})
	if !bytes.Contains(patched, []byte("Catalog Title")) {
		t.Fatal("ApplyFB2CatalogMeta did not set title")
	}
	info := mergeInfo(ConvertInfo{Title: "File Title", Authors: []PersonName{{Last: "Old"}}, Lang: "ru"},
		ConvertInfo{Title: "Catalog Title", Authors: []PersonName{{Last: "CatalogAuthor", First: "A"}}, Lang: "ru"})
	if info.Title != "Catalog Title" || info.Authors[0].Last != "CatalogAuthor" {
		t.Fatalf("mergeInfo catalog preference = %+v", info)
	}
	epub, err := FB2ToEPUB(patched, info)
	if err != nil {
		t.Fatal(err)
	}
	if len(epub) < 100 {
		t.Fatalf("epub too small: %d", len(epub))
	}
}
