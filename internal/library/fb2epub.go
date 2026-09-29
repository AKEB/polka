package library

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"html"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// epubBook is the intermediate book used to write an EPUB.
type epubBook struct {
	Title    string
	Authors  []PersonName
	Lang     string
	Series   string
	SeqNum   int
	Chapters []FB2Chapter
	Notes    map[string]string
	Images   []epubImage
	Cover    string // image Name used as the cover
	Source   []byte // original bytes, for a stable identifier
}

type epubImage struct {
	Name string
	Mime string
	Data []byte
}

type chapterFile struct {
	ID, Href, Title, HTML string
}

var fb2NoteRefRe = regexp.MustCompile(`<a data-note="([^"]*)">`)

// FB2ToEPUB converts a FictionBook 2 file into an EPUB 3 package.
func FB2ToEPUB(data []byte, info ConvertInfo) ([]byte, error) {
	meta, err := ParseFB2(bytes.NewReader(data), true)
	if err != nil {
		return nil, fmt.Errorf("fb2 meta: %w", err)
	}
	fileInfo := ConvertInfo{
		Title:   meta.Title,
		Authors: meta.Authors,
		Lang:    meta.Lang,
		Series:  meta.Series,
		SeqNum:  meta.SeriesNum,
	}
	info = mergeInfo(fileInfo, info)

	binaries, err := ExtractAllBinaries(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("fb2 binaries: %w", err)
	}

	used := map[string]int{}
	imgName := make(map[string]string, len(binaries))
	images := make([]epubImage, 0, len(binaries)+1)
	for _, b := range binaries {
		name := uniqueImageName(b.ID, b.Mime, used)
		imgName[b.ID] = name
		images = append(images, epubImage{Name: name, Mime: mimeOr(b.Mime, name), Data: b.Data})
	}

	// Cover extracted by ParseFB2 but missing from <binary> (unusual).
	if len(meta.Cover) > 0 && meta.CoverID != "" && imgName[meta.CoverID] == "" {
		name := uniqueImageName(meta.CoverID, meta.CoverMime, used)
		imgName[meta.CoverID] = name
		images = append(images, epubImage{Name: name, Mime: mimeOr(meta.CoverMime, name), Data: meta.Cover})
	}

	text, err := ParseFB2Text(bytes.NewReader(data), func(id string) string {
		if n, ok := imgName[id]; ok {
			return "images/" + n
		}
		return "images/" + uniqueImageName(id, "", used)
	})
	if err != nil {
		return nil, fmt.Errorf("fb2 text: %w", err)
	}

	cover := ""
	if meta.CoverID != "" {
		cover = imgName[meta.CoverID]
	}
	if cover == "" && len(images) > 0 {
		cover = images[0].Name
	}

	return buildEPUB(epubBook{
		Title:    info.Title,
		Authors:  info.Authors,
		Lang:     info.Lang,
		Series:   info.Series,
		SeqNum:   info.SeqNum,
		Chapters: text.Chapters,
		Notes:    text.Notes,
		Images:   images,
		Cover:    cover,
		Source:   data,
	})
}

func uniqueImageName(id, mime string, used map[string]int) string {
	name := safeImageName(id, mime)
	used[name]++
	if used[name] == 1 {
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%d%s", base, used[name], ext)
}

func safeImageName(id, mime string) string {
	id = strings.TrimPrefix(id, "#")
	id = path.Base(strings.ReplaceAll(id, "\\", "/"))
	var b strings.Builder
	for _, r := range id {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), ".")
	if name == "" {
		name = "img"
	}
	if path.Ext(name) == "" {
		name += mimeExt(mime)
	}
	return name
}

func mimeExt(mime string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0])) {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func mimeOr(mime, name string) string {
	mime = strings.TrimSpace(strings.Split(mime, ";")[0])
	if mime != "" {
		return mime
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func buildEPUB(book epubBook) ([]byte, error) {
	if len(book.Chapters) == 0 {
		return nil, fmt.Errorf("epub: no chapters")
	}
	uid := contentUUID(book.Source, book.Title)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	store := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	deflate := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}

	if err := store("mimetype", []byte("application/epub+zip")); err != nil {
		return nil, err
	}
	if err := deflate("META-INF/container.xml", []byte(epubContainer)); err != nil {
		return nil, err
	}

	files := make([]chapterFile, 0, len(book.Chapters)+1)
	for i, ch := range book.Chapters {
		href := fmt.Sprintf("chapter-%03d.xhtml", i+1)
		body := fb2NoteRefRe.ReplaceAllString(ch.HTML, `<a href="notes.xhtml#$1">`)
		files = append(files, chapterFile{
			ID:    fmt.Sprintf("ch%d", i+1),
			Href:  href,
			Title: chapterNavTitle(ch, i),
			HTML:  wrapXHTML(ch.Title, body, book.Lang),
		})
	}
	if len(book.Notes) > 0 {
		files = append(files, chapterFile{
			ID:    "notes",
			Href:  "notes.xhtml",
			Title: "Notes",
			HTML:  wrapXHTML("Notes", notesHTML(book.Notes), book.Lang),
		})
	}

	if err := deflate("OEBPS/nav.xhtml", []byte(epubNav(book.Title, files))); err != nil {
		return nil, err
	}
	if err := deflate("OEBPS/style.css", []byte(epubCSS)); err != nil {
		return nil, err
	}
	for _, f := range files {
		if err := deflate("OEBPS/"+f.Href, []byte(f.HTML)); err != nil {
			return nil, err
		}
	}
	for _, img := range book.Images {
		if err := deflate("OEBPS/images/"+img.Name, img.Data); err != nil {
			return nil, err
		}
	}
	if err := deflate("OEBPS/content.opf", []byte(epubOPF(book, uid, files))); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func chapterNavTitle(ch FB2Chapter, i int) string {
	if t := strings.TrimSpace(ch.Title); t != "" {
		return t
	}
	return fmt.Sprintf("%d", i+1)
}

func notesHTML(notes map[string]string) string {
	ids := make([]string, 0, len(notes))
	for id := range notes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var sb strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&sb, `<div id="%s">%s</div>`, html.EscapeString(id), notes[id])
	}
	return sb.String()
}

func wrapXHTML(title, body, lang string) string {
	if lang == "" {
		lang = "en"
	}
	h := ""
	if strings.TrimSpace(title) != "" {
		h = "<h1>" + html.EscapeString(title) + "</h1>\n"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" lang="` + html.EscapeString(lang) + `" xml:lang="` + html.EscapeString(lang) + `">
<head>
<meta charset="utf-8"/>
<title>` + html.EscapeString(title) + `</title>
<link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
` + h + body + `
</body>
</html>
`
}

func epubNav(bookTitle string, files []chapterFile) string {
	var items strings.Builder
	for _, f := range files {
		fmt.Fprintf(&items, "      <li><a href=%q>%s</a></li>\n", f.Href, html.EscapeString(f.Title))
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>` + html.EscapeString(bookTitle) + `</title></head>
<body>
  <nav epub:type="toc" id="toc">
    <h1>` + html.EscapeString(bookTitle) + `</h1>
    <ol>
` + items.String() + `    </ol>
  </nav>
</body>
</html>
`
}

func epubOPF(book epubBook, uid string, files []chapterFile) string {
	lang := book.Lang
	if lang == "" {
		lang = "en"
	}
	var meta, manifest, spine strings.Builder
	fmt.Fprintf(&meta, "    <dc:identifier id=\"pub-id\">urn:uuid:%s</dc:identifier>\n", uid)
	fmt.Fprintf(&meta, "    <dc:title>%s</dc:title>\n", html.EscapeString(book.Title))
	fmt.Fprintf(&meta, "    <dc:language>%s</dc:language>\n", html.EscapeString(lang))
	fmt.Fprintf(&meta, "    <meta property=\"dcterms:modified\">2020-01-01T00:00:00Z</meta>\n")
	for i, a := range book.Authors {
		name := a.String()
		if name == "" {
			continue
		}
		id := fmt.Sprintf("creator%d", i)
		fmt.Fprintf(&meta, "    <dc:creator id=%q>%s</dc:creator>\n", id, html.EscapeString(name))
		if fa := a.FileAs(); fa != "" && fa != name {
			fmt.Fprintf(&meta, "    <meta refines=\"#%s\" property=\"file-as\">%s</meta>\n", id, html.EscapeString(fa))
		}
	}
	if book.Series != "" {
		fmt.Fprintf(&meta, "    <meta property=\"belongs-to-collection\" id=\"series\">%s</meta>\n", html.EscapeString(book.Series))
		meta.WriteString("    <meta refines=\"#series\" property=\"collection-type\">series</meta>\n")
		if book.SeqNum > 0 {
			fmt.Fprintf(&meta, "    <meta refines=\"#series\" property=\"group-position\">%d</meta>\n", book.SeqNum)
		}
	}
	if book.Cover != "" {
		meta.WriteString("    <meta name=\"cover\" content=\"cover-image\"/>\n")
	}

	manifest.WriteString(`    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")
	manifest.WriteString(`    <item id="css" href="style.css" media-type="text/css"/>` + "\n")
	for _, f := range files {
		fmt.Fprintf(&manifest, `    <item id="%s" href="%s" media-type="application/xhtml+xml"/>`+"\n", f.ID, f.Href)
		fmt.Fprintf(&spine, `    <itemref idref="%s"/>`+"\n", f.ID)
	}
	for i, img := range book.Images {
		id := fmt.Sprintf("img%d", i)
		props := ""
		if img.Name == book.Cover {
			id = "cover-image"
			props = ` properties="cover-image"`
		}
		fmt.Fprintf(&manifest, `    <item id="%s" href="images/%s" media-type="%s"%s/>`+"\n",
			id, img.Name, img.Mime, props)
	}

	return `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="pub-id" version="3.0" xml:lang="` + html.EscapeString(lang) + `">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + meta.String() + `  </metadata>
  <manifest>
` + manifest.String() + `  </manifest>
  <spine>
` + spine.String() + `  </spine>
</package>
`
}

const epubContainer = `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`

const epubCSS = `body { font-family: serif; line-height: 1.45; margin: 1em; }
h1 { font-size: 1.5em; margin: 1em 0 0.6em; }
p { margin: 0 0 0.6em; text-indent: 1.2em; }
img { max-width: 100%; height: auto; }
.fb2-epigraph { font-style: italic; margin: 1em 2em; text-indent: 0; }
.fb2-poem, .fb2-cite { margin: 1em 2em; }
.fb2-verse { text-indent: 0; margin: 0; }
.fb2-note-ref { font-size: 0.8em; }
`

func contentUUID(source []byte, title string) string {
	h := sha256.New()
	if len(source) > 0 {
		h.Write(source)
	} else {
		h.Write([]byte(title))
	}
	sum := h.Sum(nil)
	sum[6] = (sum[6] & 0x0f) | 0x50 // UUID variant-ish v5
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
