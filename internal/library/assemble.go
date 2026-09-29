package library

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bodgit/sevenzip"
)

// AssembledFB2 returns an FB2 with Flibusta sidecar cover/images embedded as
// <binary> elements. Optimized dumps keep illustrations in images/ and covers
// in covers/; the raw FB2 only has l:href="#…" references.
func (l *Library) AssembledFB2(folder, file string) ([]byte, error) {
	rc, _, err := l.Open(folder, file, "fb2")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return l.enrichFB2(data, folder, file), nil
}

// BookBytes returns book file bytes, assembling Flibusta sidecars for FB2.
func (l *Library) BookBytes(folder, file, ext string) ([]byte, error) {
	if strings.EqualFold(ext, "fb2") {
		return l.AssembledFB2(folder, file)
	}
	rc, _, err := l.Open(folder, file, ext)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (l *Library) enrichFB2(data []byte, folder, file string) []byte {
	refs := fb2ImageRefs(data)
	images := l.sidecarImages(folder, file)
	have := existingBinaryIDs(data)

	var add []FB2Binary
	needCoverpage := false
	for _, id := range refs {
		if have[id] {
			continue
		}
		if img, ok := images[id]; ok {
			add = append(add, FB2Binary{ID: id, Mime: img.mime, Data: img.data})
			continue
		}
		if isCoverID(id) {
			if cover, mime, ok := l.sidecarCover(folder, file); ok {
				cover, mime = normalizeCover(cover, mime)
				add = append(add, FB2Binary{ID: id, Mime: mime, Data: cover})
			}
		}
	}
	// No cover href at all, but sidecar cover exists — add coverpage + binary.
	if !refsHasCover(refs) {
		if !have["cover"] {
			if cover, mime, ok := l.sidecarCover(folder, file); ok {
				cover, mime = normalizeCover(cover, mime)
				add = append(add, FB2Binary{ID: "cover", Mime: mime, Data: cover})
				needCoverpage = true
			}
		}
	}
	if len(add) == 0 {
		return data
	}
	return InjectFB2Binaries(data, add, needCoverpage)
}

func isCoverID(id string) bool {
	id = strings.ToLower(id)
	return id == "cover" || strings.HasPrefix(id, "cover.")
}

func refsHasCover(refs []string) bool {
	for _, id := range refs {
		if isCoverID(id) {
			return true
		}
	}
	return false
}

var fb2HrefRe = regexp.MustCompile(`(?i)(?:l:|xlink:)?href="#([^"]+)"`)

func fb2ImageRefs(data []byte) []string {
	matches := fb2HrefRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		id := string(m[1])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

var fb2BinaryIDRe = regexp.MustCompile(`(?is)<binary\b[^>]*\bid="([^"]+)"`)

func existingBinaryIDs(data []byte) map[string]bool {
	out := map[string]bool{}
	for _, m := range fb2BinaryIDRe.FindAllSubmatch(data, -1) {
		out[string(m[1])] = true
	}
	return out
}

// InjectFB2Binaries appends missing <binary> elements before </FictionBook>.
// When addCoverpage is true and no coverpage exists, one is inserted into title-info.
func InjectFB2Binaries(data []byte, binaries []FB2Binary, addCoverpage bool) []byte {
	if len(binaries) == 0 && !addCoverpage {
		return data
	}
	have := existingBinaryIDs(data)
	var buf bytes.Buffer
	for _, b := range binaries {
		if b.ID == "" || len(b.Data) == 0 || have[b.ID] {
			continue
		}
		mime := b.Mime
		if mime == "" {
			mime = sniffImageMime(b.Data)
		}
		fmt.Fprintf(&buf, `<binary id="%s" content-type="%s">`, xmlAttr(b.ID), xmlAttr(mime))
		buf.WriteString(base64.StdEncoding.EncodeToString(b.Data))
		buf.WriteString("</binary>\n")
		have[b.ID] = true
	}
	payload := buf.Bytes()

	out := data
	if addCoverpage && !hasFB2Tag(out, "<coverpage") {
		out = insertFB2Coverpage(out, "cover")
	}
	if len(payload) == 0 {
		return out
	}

	// Find </FictionBook> without bytes.ToLower on the whole document:
	// Unicode case folding can change byte length, so an index into the
	// lowercased copy is not valid for the original slice (panic).
	idx := indexFB2Close(out)
	if idx < 0 {
		return append(out, payload...)
	}
	var assembled bytes.Buffer
	assembled.Grow(len(out) + len(payload))
	assembled.Write(out[:idx])
	assembled.Write(payload)
	assembled.Write(out[idx:])
	return assembled.Bytes()
}

var titleInfoCloseRe = regexp.MustCompile(`(?i)</title-info>`)

func insertFB2Coverpage(data []byte, coverID string) []byte {
	tag := []byte(`<coverpage><image l:href="#` + coverID + `"/></coverpage>`)
	loc := titleInfoCloseRe.FindIndex(data)
	if loc == nil {
		return data
	}
	var out bytes.Buffer
	out.Grow(len(data) + len(tag))
	out.Write(data[:loc[0]])
	out.Write(tag)
	out.Write(data[loc[0]:])
	return out.Bytes()
}

func indexFB2Close(data []byte) int {
	const tag = "</fictionbook>"
	n := len(tag)
	for i := len(data) - n; i >= 0; i-- {
		if bytes.EqualFold(data[i:i+n], []byte(tag)) {
			return i
		}
	}
	return -1
}

func hasFB2Tag(data []byte, tag string) bool {
	n := len(tag)
	needle := []byte(tag)
	for i := 0; i+n <= len(data); i++ {
		if bytes.EqualFold(data[i:i+n], needle) {
			return true
		}
	}
	return false
}

type sidecarImg struct {
	data []byte
	mime string
}

// sidecarImages loads illustrations from images/<archive>.zip|7z.
// Entries are named <bookId>/<n> (Flibusta layout); keys are the <n> ids.
func (l *Library) sidecarImages(folder, bookID string) map[string]sidecarImg {
	out := map[string]sidecarImg{}
	if l == nil || bookID == "" {
		return out
	}
	base := strings.TrimSuffix(filepath.Base(folder), filepath.Ext(folder))
	if base == "" {
		return out
	}
	prefix := bookID + "/"
	for _, aext := range []string{".zip", ".7z"} {
		ap := filepath.Join(l.root, "images", base+aext)
		if _, err := os.Stat(ap); err != nil {
			continue
		}
		load := func(name string, open func() (io.ReadCloser, error)) {
			if !strings.HasPrefix(name, prefix) {
				return
			}
			id := strings.TrimPrefix(name, prefix)
			if id == "" || strings.Contains(id, "/") {
				return
			}
			rc, err := open()
			if err != nil {
				return
			}
			defer rc.Close()
			raw, err := io.ReadAll(rc)
			if err != nil || len(raw) == 0 {
				return
			}
			mime := sniffImageMime(raw)
			raw, mime = normalizeCover(raw, mime)
			out[id] = sidecarImg{data: raw, mime: mime}
		}
		if strings.EqualFold(aext, ".7z") {
			zr, err := sevenzip.OpenReader(ap)
			if err != nil {
				continue
			}
			for _, e := range zr.File {
				e := e
				load(e.Name, e.Open)
			}
			zr.Close()
			if len(out) > 0 {
				return out
			}
			continue
		}
		zr, err := zip.OpenReader(ap)
		if err != nil {
			continue
		}
		for _, e := range zr.File {
			e := e
			load(e.Name, e.Open)
		}
		zr.Close()
		if len(out) > 0 {
			return out
		}
	}
	return out
}

func (l *Library) sidecarImage(folder, bookID, imgID string) ([]byte, string, bool) {
	imgs := l.sidecarImages(folder, bookID)
	if img, ok := imgs[imgID]; ok {
		return img.data, img.mime, true
	}
	return nil, "", false
}
