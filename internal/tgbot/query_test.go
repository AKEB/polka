package tgbot

import (
	"testing"

	"github.com/vestigiumincaligne/polka/internal/store"
)

func TestParseQuery(t *testing.T) {
	cases := []struct {
		in   string
		want []store.SearchFilter
	}{
		{"Гарри Поттер", []store.SearchFilter{{Value: "Гарри Поттер"}}},
		{"title:Гарри", []store.SearchFilter{{Field: "title", Value: "Гарри"}}},
		{"title:=Гарри Поттер", []store.SearchFilter{{Field: "title", Exact: true, Value: "Гарри Поттер"}}},
		{
			"series:=Дозоры and author:Лукьяненко",
			[]store.SearchFilter{
				{Field: "series", Exact: true, Value: "Дозоры"},
				{Field: "author", Value: "Лукьяненко"},
			},
		},
		{
			"TITLE:Foo AND author:=Bar",
			[]store.SearchFilter{
				{Field: "title", Value: "Foo"},
				{Field: "author", Exact: true, Value: "Bar"},
			},
		},
	}
	for _, tc := range cases {
		got := ParseQuery(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("%q: got %#v want %#v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q [%d]: %#v != %#v", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
