package library

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const flibustaFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
  <title-info>
    <author><first-name>A</first-name><last-name>B</last-name></author>
    <book-title>Sidecar Book</book-title>
    <coverpage><image l:href="#cover"/></coverpage>
    <lang>ru</lang>
  </title-info>
</description>
<body><section><p>Hi</p><image l:href="#1"/><image l:href="#2"/></section></body>
</FictionBook>`

const flibustaNoCoverFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
  <title-info>
    <author><first-name>A</first-name><last-name>B</last-name></author>
    <book-title>No Cover Ref</book-title>
    <lang>ru</lang>
  </title-info>
</description>
<body><section><p>Hi</p><image l:href="#1"/></section></body>
</FictionBook>`

func TestAssembledFB2InjectsSidecars(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	jpeg := []byte("\xFF\xD8\xFFjpeg-cover")
	img1 := []byte("\xFF\xD8\xFFjpeg-1")
	img2 := []byte("\xFF\xD8\xFFjpeg-2")

	writeZip(t, filepath.Join(root, "fb2-1-10.zip"), map[string][]byte{
		"42.fb2": []byte(flibustaFB2),
	})
	os.MkdirAll(filepath.Join(root, "covers"), 0o755)
	os.MkdirAll(filepath.Join(root, "images"), 0o755)
	writeZip(t, filepath.Join(root, "covers", "fb2-1-10.zip"), map[string][]byte{
		"42": jpeg,
	})
	writeZip(t, filepath.Join(root, "images", "fb2-1-10.zip"), map[string][]byte{
		"42/1": img1,
		"42/2": img2,
	})

	data, err := l.AssembledFB2("fb2-1-10.zip", "42")
	if err != nil {
		t.Fatal(err)
	}
	bins, err := ExtractAllBinaries(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string][]byte{}
	for _, b := range bins {
		byID[b.ID] = b.Data
	}
	if !bytes.Equal(byID["cover"], jpeg) {
		t.Fatalf("cover binary: %q", byID["cover"])
	}
	if !bytes.Equal(byID["1"], img1) || !bytes.Equal(byID["2"], img2) {
		t.Fatalf("illustration binaries: %#v", byID)
	}

	// EPUB conversion must carry the injected binaries.
	epub, err := l.ConvertBook("fb2-1-10.zip", "42", "fb2", "epub", ConvertInfo{Title: "Sidecar Book"})
	if err != nil {
		t.Fatal(err)
	}
	if len(epub) < 100 || !bytes.Contains(epub, []byte("PK")) {
		t.Fatalf("epub looks wrong: %d bytes", len(epub))
	}
}

func TestAssembledFB2AddsCoverpage(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	jpeg := []byte("\xFF\xD8\xFFjpeg-cover")
	img1 := []byte("\xFF\xD8\xFFjpeg-1")

	writeZip(t, filepath.Join(root, "a.zip"), map[string][]byte{"7.fb2": []byte(flibustaNoCoverFB2)})
	os.MkdirAll(filepath.Join(root, "covers"), 0o755)
	os.MkdirAll(filepath.Join(root, "images"), 0o755)
	writeZip(t, filepath.Join(root, "covers", "a.zip"), map[string][]byte{"7": jpeg})
	writeZip(t, filepath.Join(root, "images", "a.zip"), map[string][]byte{"7/1": img1})

	data, err := l.AssembledFB2("a.zip", "7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `<coverpage><image l:href="#cover"/>`) {
		t.Fatalf("missing coverpage: %s", data)
	}
	bins, _ := ExtractAllBinaries(bytes.NewReader(data))
	if len(bins) < 2 {
		t.Fatalf("want cover+1, got %d", len(bins))
	}
}

func TestBinaryFallsBackToSidecar(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	jpeg := []byte("\xFF\xD8\xFFjpeg-cover")
	img1 := []byte("\xFF\xD8\xFFjpeg-1")
	writeZip(t, filepath.Join(root, "a.zip"), map[string][]byte{"7.fb2": []byte(flibustaNoCoverFB2)})
	os.MkdirAll(filepath.Join(root, "covers"), 0o755)
	os.MkdirAll(filepath.Join(root, "images"), 0o755)
	writeZip(t, filepath.Join(root, "covers", "a.zip"), map[string][]byte{"7": jpeg})
	writeZip(t, filepath.Join(root, "images", "a.zip"), map[string][]byte{"7/1": img1})

	data, mime, err := l.Binary("a.zip", "7", "fb2", "1")
	if err != nil || mime != "image/jpeg" || !bytes.Equal(data, img1) {
		t.Fatalf("sidecar image: %q %v %v", mime, len(data), err)
	}
	data, mime, err = l.Binary("a.zip", "7", "fb2", "cover")
	if err != nil || !bytes.Equal(data, jpeg) {
		t.Fatalf("sidecar cover via Binary: %q %v", mime, err)
	}
}

func TestInjectFB2BinariesSkipsExisting(t *testing.T) {
	existing, _ := base64.StdEncoding.DecodeString(tinyPNG)
	out := InjectFB2Binaries([]byte(testFB2), []FB2Binary{
		{ID: "cover.png", Mime: "image/png", Data: []byte("new")},
		{ID: "extra", Mime: "image/jpeg", Data: []byte("\xFF\xD8\xFFx")},
	}, false)
	bins, _ := ExtractAllBinaries(bytes.NewReader(out))
	byID := map[string][]byte{}
	for _, b := range bins {
		byID[b.ID] = b.Data
	}
	if !bytes.Equal(byID["cover.png"], existing) {
		t.Fatal("must not replace existing binary")
	}
	if !bytes.Equal(byID["extra"], []byte("\xFF\xD8\xFFx")) {
		t.Fatal("must add missing binary")
	}
}
