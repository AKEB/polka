package tgbot

import (
	"strings"
	"testing"
)

func TestFitHTMLKeepsTagsBalanced(t *testing.T) {
	src := "<b>1.</b> 📚 Very long title that will be truncated somewhere in the middle of this sentence so we hit the limit"
	got := fitHTML(src, 40)
	if !balancedTG(got) {
		t.Fatalf("unbalanced HTML: %q", got)
	}
}

func TestFitHTMLClosesNestedItalic(t *testing.T) {
	src := "<b>Title</b>\n<i>" + strings.Repeat("annotation ", 80) + "</i>\npick"
	got := fitHTML(src, 80)
	if !balancedTG(got) {
		t.Fatalf("unbalanced nested HTML: %q", got)
	}
	if strings.Contains(got, "<i>") && !strings.Contains(got, "</i>") {
		t.Fatalf("missing </i>: %q", got)
	}
}

func TestFitHTMLCutsIncompleteTag(t *testing.T) {
	// Simulate a mid-tag cut: open <i> without closing before limit.
	src := "<b>Hello</b>\n<i>" + strings.Repeat("x", 100)
	got := fitHTML(src, 30)
	if !balancedTG(got) {
		t.Fatalf("unbalanced after mid-content cut: %q", got)
	}
}

func TestCloseTGTagsNested(t *testing.T) {
	got := closeTGTags("<b><i>x")
	want := "<b><i>x</i></b>"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func balancedTG(s string) bool {
	allowed := map[string]bool{
		"b": true, "i": true, "u": true, "s": true,
		"code": true, "pre": true, "tg-spoiler": true,
	}
	var stack []string
	for i := 0; i < len(s); {
		if s[i] != '<' {
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			return false
		}
		tag := s[i+1 : i+end]
		i += end + 1
		closing := false
		if strings.HasPrefix(tag, "/") {
			closing = true
			tag = tag[1:]
		}
		if !allowed[tag] {
			continue
		}
		if closing {
			if len(stack) == 0 || stack[len(stack)-1] != tag {
				return false
			}
			stack = stack[:len(stack)-1]
			continue
		}
		stack = append(stack, tag)
	}
	return len(stack) == 0
}
