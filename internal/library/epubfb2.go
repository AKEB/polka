package library

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// fb2Book is the intermediate book used to write an FB2.
type fb2Book struct {
	Title    string
	Authors  []PersonName
	Lang     string
	Series   string
	SeqNum   int
	Genres   []string
	Chapters []FB2Chapter
	Notes    map[string]string
	Images   []FB2Binary
	CoverID  string
	Source   []byte
}

// EPUBToFB2 converts an EPUB into a FictionBook 2 file.
func EPUBToFB2(data []byte, info ConvertInfo) ([]byte, error) {
	r := bytes.NewReader(data)
	meta, err := parseOPFFromEPUB(r, int64(len(data)))
	if err != nil {
		// Still convertible from catalog metadata if OPF is odd.
		meta = &EPUBMeta{}
	}
	fileInfo := ConvertInfo{
		Title:   meta.Title,
		Authors: authorsFromEPUB(meta.Authors),
		Lang:    meta.Language,
	}
	info = mergeInfo(fileInfo, info)

	seen := map[string]string{} // archive path -> binary id
	var images []FB2Binary
	imgURL := func(p string) string {
		if id, ok := seen[p]; ok {
			return id
		}
		id := fmt.Sprintf("i%d%s", len(images), imageExt(p))
		seen[p] = id
		images = append(images, FB2Binary{ID: id}) // data filled below
		return id
	}

	text, err := EPUBText(bytes.NewReader(data), int64(len(data)), imgURL)
	if err != nil {
		return nil, fmt.Errorf("epub text: %w", err)
	}

	for p, id := range seen {
		raw, mime, err := EPUBBinary(bytes.NewReader(data), int64(len(data)), p)
		if err != nil {
			continue
		}
		for i := range images {
			if images[i].ID == id {
				images[i].Data = raw
				images[i].Mime = mimeOr(mime, id)
				break
			}
		}
	}

	coverID := ""
	if cover, mime, err := EPUBCover(bytes.NewReader(data), int64(len(data))); err == nil && len(cover) > 0 {
		// Reuse an already-extracted image if the bytes match; otherwise add it.
		for _, img := range images {
			if bytes.Equal(img.Data, cover) {
				coverID = img.ID
				break
			}
		}
		if coverID == "" {
			coverID = "cover" + mimeExt(mime)
			images = append(images, FB2Binary{ID: coverID, Mime: mimeOr(mime, coverID), Data: cover})
		}
	}

	return buildFB2(fb2Book{
		Title:    info.Title,
		Authors:  info.Authors,
		Lang:     info.Lang,
		Series:   info.Series,
		SeqNum:   info.SeqNum,
		Genres:   GenresFromSubjects(meta.Subjects),
		Chapters: text.Chapters,
		Notes:    text.Notes,
		Images:   images,
		CoverID:  coverID,
		Source:   data,
	})
}

func imageExt(p string) string {
	if u, err := url.PathUnescape(p); err == nil {
		p = u
	}
	ext := strings.ToLower(path.Ext(p))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		if ext == ".jpeg" {
			return ".jpg"
		}
		return ext
	}
	return ".jpg"
}

func parseOPFFromEPUB(r io.ReaderAt, size int64) (*EPUBMeta, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	opfPath, err := epubRootFile(zr)
	if err != nil {
		return nil, err
	}
	rc, err := openZipFile(zr, opfPath)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return parseOPF(rc)
}

func buildFB2(book fb2Book) ([]byte, error) {
	if len(book.Chapters) == 0 {
		return nil, fmt.Errorf("fb2: no chapters")
	}
	uid := contentUUID(book.Source, book.Title)
	lang := book.Lang
	if lang == "" {
		lang = "en"
	}

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">` + "\n")
	sb.WriteString("<description>\n<title-info>\n")
	if len(book.Genres) == 0 {
		sb.WriteString("<genre>prose</genre>\n")
	}
	for _, g := range book.Genres {
		sb.WriteString("<genre>" + xmlText(g) + "</genre>\n")
	}
	if len(book.Authors) == 0 {
		sb.WriteString("<author><last-name>Unknown</last-name></author>\n")
	}
	for _, a := range book.Authors {
		sb.WriteString("<author>")
		if a.First != "" {
			sb.WriteString("<first-name>" + xmlText(a.First) + "</first-name>")
		}
		if a.Middle != "" {
			sb.WriteString("<middle-name>" + xmlText(a.Middle) + "</middle-name>")
		}
		if a.Last != "" {
			sb.WriteString("<last-name>" + xmlText(a.Last) + "</last-name>")
		}
		if a.First == "" && a.Middle == "" && a.Last == "" {
			sb.WriteString("<last-name>Unknown</last-name>")
		}
		sb.WriteString("</author>\n")
	}
	sb.WriteString("<book-title>" + xmlText(book.Title) + "</book-title>\n")
	if book.CoverID != "" {
		sb.WriteString(`<coverpage><image l:href="#` + xmlText(book.CoverID) + `"/></coverpage>` + "\n")
	}
	sb.WriteString("<lang>" + xmlText(lang) + "</lang>\n")
	if book.Series != "" {
		fmt.Fprintf(&sb, `<sequence name="%s"`, xmlAttr(book.Series))
		if book.SeqNum > 0 {
			fmt.Fprintf(&sb, ` number="%d"`, book.SeqNum)
		}
		sb.WriteString("/>\n")
	}
	sb.WriteString("</title-info>\n")
	sb.WriteString("<document-info>\n")
	sb.WriteString("<program-used>Polka</program-used>\n")
	sb.WriteString("<id>" + xmlText(uid) + "</id>\n")
	sb.WriteString("<version>1.0</version>\n")
	sb.WriteString("</document-info>\n</description>\n")

	sb.WriteString("<body>\n")
	for _, ch := range book.Chapters {
		sb.WriteString("<section>\n")
		if t := strings.TrimSpace(ch.Title); t != "" {
			sb.WriteString("<title><p>" + xmlText(t) + "</p></title>\n")
		}
		body, err := htmlToFB2(stripLeadingHeading(ch.HTML, ch.Title))
		if err != nil {
			return nil, err
		}
		sb.WriteString(body)
		sb.WriteString("</section>\n")
	}
	sb.WriteString("</body>\n")

	if len(book.Notes) > 0 {
		sb.WriteString(`<body name="notes">` + "\n")
		ids := make([]string, 0, len(book.Notes))
		for id := range book.Notes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			body, err := htmlToFB2(book.Notes[id])
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&sb, `<section id="%s">%s</section>`+"\n", xmlAttr(id), body)
		}
		sb.WriteString("</body>\n")
	}

	for _, img := range book.Images {
		if len(img.Data) == 0 {
			continue
		}
		mime := mimeOr(img.Mime, img.ID)
		fmt.Fprintf(&sb, `<binary id="%s" content-type="%s">`, xmlAttr(img.ID), xmlAttr(mime))
		sb.WriteString(base64.StdEncoding.EncodeToString(img.Data))
		sb.WriteString("</binary>\n")
	}

	sb.WriteString("</FictionBook>\n")
	return []byte(sb.String()), nil
}

func xmlText(s string) string {
	return html.EscapeString(s)
}

func xmlAttr(s string) string {
	return html.EscapeString(s)
}

func stripLeadingHeading(htmlStr, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return htmlStr
	}
	// Drop a leading h1–h4 whose text equals the section title so it is
	// not duplicated after we write <title>.
	s := strings.TrimSpace(htmlStr)
	for _, tag := range []string{"h1", "h2", "h3", "h4"} {
		open := "<" + tag + ">"
		close := "</" + tag + ">"
		if strings.HasPrefix(s, open) {
			end := strings.Index(s, close)
			if end < 0 {
				break
			}
			inner := strings.TrimSpace(htmlUnescape(s[len(open):end]))
			if inner == title {
				return strings.TrimSpace(s[end+len(close):])
			}
		}
	}
	return htmlStr
}

var htmlToFB2Tags = map[string]string{
	"p":          "p",
	"em":         "emphasis",
	"i":          "emphasis",
	"strong":     "strong",
	"b":          "strong",
	"s":          "strikethrough",
	"strike":     "strikethrough",
	"sub":        "sub",
	"sup":        "sup",
	"code":       "code",
	"pre":        "code",
	"blockquote": "cite",
	"cite":       "cite",
	"h1":         "subtitle",
	"h2":         "subtitle",
	"h3":         "subtitle",
	"h4":         "subtitle",
	"h5":         "subtitle",
	"h6":         "subtitle",
	"table":      "table",
	"tr":         "tr",
	"td":         "td",
	"th":         "th",
}

func htmlToFB2(fragment string) (string, error) {
	nodes, err := nethtml.ParseFragment(strings.NewReader(fragment), &nethtml.Node{
		Type:     nethtml.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err != nil {
		return "", fmt.Errorf("html: %w", err)
	}
	var sb strings.Builder
	for _, n := range nodes {
		renderFB2(&sb, n)
	}
	if sb.Len() == 0 {
		// A chapter of only whitespace still needs a paragraph.
		return "<p/>", nil
	}
	return sb.String(), nil
}

func renderFB2(sb *strings.Builder, n *nethtml.Node) {
	switch n.Type {
	case nethtml.TextNode:
		if t := n.Data; t != "" {
			sb.WriteString(html.EscapeString(t))
		}
	case nethtml.ElementNode:
		tag := strings.ToLower(n.Data)
		switch tag {
		case "script", "style", "head":
			return
		case "br", "hr":
			sb.WriteString("<empty-line/>")
			return
		case "img":
			src := ""
			for _, a := range n.Attr {
				if a.Key == "src" {
					src = a.Val
					break
				}
			}
			src = strings.TrimPrefix(src, "#")
			if src != "" {
				fmt.Fprintf(sb, `<image l:href="#%s"/>`, xmlAttr(src))
			}
			return
		case "li":
			sb.WriteString("<p>")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				renderFB2(sb, c)
			}
			sb.WriteString("</p>")
			return
		}
		if fb2, ok := htmlToFB2Tags[tag]; ok {
			sb.WriteString("<" + fb2 + ">")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				renderFB2(sb, c)
			}
			sb.WriteString("</" + fb2 + ">")
			return
		}
		// Unwrap unknown wrappers (div, span, section, a, ul, …).
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderFB2(sb, c)
		}
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderFB2(sb, c)
		}
	}
}
