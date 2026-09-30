package library

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

var (
	titleInfoBlockRe = regexp.MustCompile(`(?is)<title-info\b[^>]*>.*?</title-info>`)
	bookTitleRe      = regexp.MustCompile(`(?is)(<book-title\b[^>]*>)(.*?)(</book-title>)`)
	langInTitleRe    = regexp.MustCompile(`(?is)(<lang\b[^>]*>)(.*?)(</lang>)`)
	authorInTitleRe  = regexp.MustCompile(`(?is)<author\b[^>]*>.*?</author>\s*`)
	sequenceRe       = regexp.MustCompile(`(?is)<sequence\b[^>]*/>|<sequence\b[^>]*>.*?</sequence>`)
)

// ApplyFB2CatalogMeta rewrites title-info fields from catalog metadata so
// admin corrections appear in downloaded FB2 (and in FB2→EPUB conversion).
func ApplyFB2CatalogMeta(data []byte, info ConvertInfo) []byte {
	if info.Title == "" && len(info.Authors) == 0 && info.Lang == "" && info.Series == "" {
		return data
	}
	loc := titleInfoBlockRe.FindIndex(data)
	if loc == nil {
		return data
	}
	block := data[loc[0]:loc[1]]
	updated := rewriteTitleInfo(block, info)
	if bytes.Equal(block, updated) {
		return data
	}
	var out bytes.Buffer
	out.Grow(len(data) - len(block) + len(updated))
	out.Write(data[:loc[0]])
	out.Write(updated)
	out.Write(data[loc[1]:])
	return out.Bytes()
}

func rewriteTitleInfo(block []byte, info ConvertInfo) []byte {
	out := string(block)
	if info.Title != "" {
		if bookTitleRe.MatchString(out) {
			out = bookTitleRe.ReplaceAllString(out, "${1}"+xmlText(info.Title)+"${3}")
		} else {
			out = string(insertBeforeTitleInfoClose([]byte(out), []byte("<book-title>"+xmlText(info.Title)+"</book-title>\n")))
		}
	}
	if len(info.Authors) > 0 {
		out = authorInTitleRe.ReplaceAllString(out, "")
		var authors strings.Builder
		for _, a := range info.Authors {
			authors.WriteString("<author>")
			if a.First != "" {
				authors.WriteString("<first-name>" + xmlText(a.First) + "</first-name>")
			}
			if a.Middle != "" {
				authors.WriteString("<middle-name>" + xmlText(a.Middle) + "</middle-name>")
			}
			if a.Last != "" {
				authors.WriteString("<last-name>" + xmlText(a.Last) + "</last-name>")
			}
			authors.WriteString("</author>\n")
		}
		if i := strings.IndexByte(out, '>'); i >= 0 {
			out = out[:i+1] + "\n" + authors.String() + out[i+1:]
		}
	}
	if info.Lang != "" {
		if langInTitleRe.MatchString(out) {
			out = langInTitleRe.ReplaceAllString(out, "${1}"+xmlText(info.Lang)+"${3}")
		} else {
			out = string(insertBeforeTitleInfoClose([]byte(out), []byte("<lang>"+xmlText(info.Lang)+"</lang>\n")))
		}
	}
	if info.Series != "" {
		out = sequenceRe.ReplaceAllString(out, "")
		seq := fmt.Sprintf(`<sequence name="%s"`, xmlAttr(info.Series))
		if info.SeqNum > 0 {
			seq += fmt.Sprintf(` number="%d"`, info.SeqNum)
		}
		seq += "/>\n"
		out = string(insertBeforeTitleInfoClose([]byte(out), []byte(seq)))
	}
	return []byte(out)
}

func insertBeforeTitleInfoClose(block, insert []byte) []byte {
	lower := bytes.ToLower(block)
	idx := bytes.LastIndex(lower, []byte("</title-info>"))
	if idx < 0 {
		return block
	}
	var out bytes.Buffer
	out.Grow(len(block) + len(insert))
	out.Write(block[:idx])
	out.Write(insert)
	out.Write(block[idx:])
	return out.Bytes()
}

// HasCatalogOverride reports whether catalog info should rewrite file metadata.
func HasCatalogOverride(info ConvertInfo) bool {
	return strings.TrimSpace(info.Title) != "" || len(info.Authors) > 0 ||
		strings.TrimSpace(info.Lang) != "" || strings.TrimSpace(info.Series) != ""
}
