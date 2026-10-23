package wiki

import (
	"slices"
	"testing"
)

func TestParseLink(t *testing.T) {
	tests := []struct {
		url  string
		want Link
		ok   bool
	}{
		{"https://wiki.example.com/en/widgets/reports/caching", Link{Locale: "en", Path: "widgets/reports/caching"}, true},
		{"https://wiki.example.com/widgets/home", Link{Locale: "en", Path: "widgets/home"}, true},
		{"https://wiki.example.com/fr/widgets/home", Link{Locale: "fr", Path: "widgets/home"}, true},
		{"https://wiki.example.com/pt-br/widgets/home", Link{Locale: "pt-br", Path: "widgets/home"}, true},
		{"https://wiki.example.com/h/en/widgets/home", Link{Locale: "en", Path: "widgets/home"}, true},
		{"https://wiki.example.com/en/widgets/home#section", Link{Locale: "en", Path: "widgets/home"}, true},
		{"https://wiki.example.com/i/41", Link{ID: 41}, true},
		{"https://wiki.example.com/en", Link{Locale: "en", Path: "en"}, true},
		{"https://wiki.example.com/i/zero", Link{}, false},
		{"https://wiki.example.com/i/0", Link{}, false},
		{"https://wiki.example.com/t/widgets", Link{}, false},
		{"https://wiki.example.com/a/pages", Link{}, false},
		{"https://wiki.example.com/", Link{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseLink(tt.url)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseLink(%q) = %+v, %v; want %+v, %v", tt.url, got, ok, tt.want, tt.ok)
		}
	}
}

func TestLinkString(t *testing.T) {
	for link, want := range map[Link]string{
		{ID: 41}:                          "i/41",
		{Locale: "en", Path: "widgets/a"}: "en/widgets/a",
	} {
		if got := link.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", link, got, want)
		}
	}
}

func TestLinkLookups(t *testing.T) {
	tests := []struct {
		link Link
		want []Link
	}{
		{Link{ID: 41}, nil},
		{Link{Locale: "en", Path: "widgets/a"}, []Link{{Locale: "en", Path: "widgets/a"}}},
		{Link{Locale: "ai", Path: "notes"}, []Link{{Locale: "ai", Path: "notes"}, {Locale: "en", Path: "ai/notes"}}},
	}
	for _, tt := range tests {
		if got := tt.link.Lookups(); !slices.Equal(got, tt.want) {
			t.Errorf("%+v.Lookups() = %+v, want %+v", tt.link, got, tt.want)
		}
	}
}
