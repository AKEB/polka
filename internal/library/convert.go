package library

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// ConvertInfo is catalog metadata used when the source file has none
// of its own (plain text) or as a fallback for missing fields.
type ConvertInfo struct {
	Title   string
	Authors []PersonName
	Lang    string
	Series  string
	SeqNum  int
}

// CanConvert reports whether a book of source extension `from` can be
// turned into `to` (fb2 or epub). Identity (same format) is allowed.
func CanConvert(from, to string) bool {
	from, to = normExt(from), normExt(to)
	if to != "fb2" && to != "epub" {
		return false
	}
	switch from {
	case "fb2", "epub", "txt":
		return true
	}
	return false
}

// ConvertBook opens a library file and returns it in the requested
// format ("fb2" or "epub"). If the file is already in that format, the
// original bytes are returned unchanged.
func (l *Library) ConvertBook(folder, file, ext, to string, info ConvertInfo) ([]byte, error) {
	to, ext = normExt(to), normExt(ext)
	if !CanConvert(ext, to) {
		return nil, ErrUnsupportedConversion
	}
	rc, _, err := l.Open(folder, file, ext)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return Convert(data, ext, to, info)
}

// Convert turns book bytes of extension `from` into `to`.
func Convert(data []byte, from, to string, info ConvertInfo) ([]byte, error) {
	from, to = normExt(from), normExt(to)
	if !CanConvert(from, to) {
		return nil, ErrUnsupportedConversion
	}
	if from == to {
		return data, nil
	}
	switch {
	case from == "fb2" && to == "epub":
		return FB2ToEPUB(data, info)
	case from == "epub" && to == "fb2":
		return EPUBToFB2(data, info)
	case from == "txt" && to == "epub":
		return TXTToEPUB(data, info)
	case from == "txt" && to == "fb2":
		return TXTToFB2(data, info)
	}
	return nil, ErrUnsupportedConversion
}

func normExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}

func mergeInfo(file ConvertInfo, catalog ConvertInfo) ConvertInfo {
	out := file
	if out.Title == "" {
		out.Title = catalog.Title
	}
	if len(out.Authors) == 0 {
		out.Authors = catalog.Authors
	}
	if out.Lang == "" {
		out.Lang = catalog.Lang
	}
	if out.Series == "" {
		out.Series = catalog.Series
		out.SeqNum = catalog.SeqNum
	}
	if out.Lang == "" {
		out.Lang = "en"
	}
	if out.Title == "" {
		out.Title = "Untitled"
	}
	return out
}

func authorsFromEPUB(names []string) []PersonName {
	var out []PersonName
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		out = append(out, parseWesternName(n))
	}
	return out
}

// parseWesternName splits "First Middle Last" (EPUB dc:creator style).
func parseWesternName(s string) PersonName {
	fields := strings.Fields(s)
	switch len(fields) {
	case 0:
		return PersonName{}
	case 1:
		return PersonName{Last: fields[0]}
	case 2:
		return PersonName{First: fields[0], Last: fields[1]}
	default:
		return PersonName{
			First:  fields[0],
			Middle: strings.Join(fields[1:len(fields)-1], " "),
			Last:   fields[len(fields)-1],
		}
	}
}

func TXTToEPUB(data []byte, info ConvertInfo) ([]byte, error) {
	text, err := ParseTXT(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("txt: %w", err)
	}
	info = mergeInfo(ConvertInfo{}, info)
	return buildEPUB(epubBook{
		Title:    info.Title,
		Authors:  info.Authors,
		Lang:     info.Lang,
		Series:   info.Series,
		SeqNum:   info.SeqNum,
		Chapters: text.Chapters,
		Source:   data,
	})
}

func TXTToFB2(data []byte, info ConvertInfo) ([]byte, error) {
	text, err := ParseTXT(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("txt: %w", err)
	}
	info = mergeInfo(ConvertInfo{}, info)
	return buildFB2(fb2Book{
		Title:    info.Title,
		Authors:  info.Authors,
		Lang:     info.Lang,
		Series:   info.Series,
		SeqNum:   info.SeqNum,
		Chapters: text.Chapters,
		Source:   data,
	})
}
