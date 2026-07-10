package httpapi

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func mustParse(t *testing.T, s string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

func TestParseIconLinks_ordersAndSplits(t *testing.T) {
	base, _ := url.Parse("https://example.com/page")
	doc := mustParse(t, `<html><head>
		<link rel="icon" sizes="16x16" href="/small.png">
		<link rel="icon" sizes="32x32" href="/large.png">
		<link rel="apple-touch-icon" href="apple-57.png">
		<link rel="apple-touch-icon" sizes="180x180" href="https://cdn.example.com/apple-180.png">
		<link rel="mask-icon" href="/mask.svg">
		<link rel="icon" type="image/svg+xml" href="/vector.svg">
		<link rel="stylesheet" href="/x.css">
	</head></html>`)

	apple, icons := parseIconLinks(doc, base)

	// apple-touch: largest declared size first; relative href resolved against base.
	wantApple := []string{"https://cdn.example.com/apple-180.png", "https://example.com/apple-57.png"}
	if !reflect.DeepEqual(apple, wantApple) {
		t.Fatalf("apple = %v, want %v", apple, wantApple)
	}
	// raster icons: largest first; SVG and mask-icon excluded.
	wantIcons := []string{"https://example.com/large.png", "https://example.com/small.png"}
	if !reflect.DeepEqual(icons, wantIcons) {
		t.Fatalf("icons = %v, want %v", icons, wantIcons)
	}
}

func TestParseIconSize(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"any", 0},
		{"16x16", 16},
		{"180x180", 180},
		{"16x16 32x32", 32},
		{"48X48", 0}, // uppercase X is lowercased before parse; "48x48" via sizes attr
	} {
		if got := parseIconSize(tc.in); got != tc.want {
			t.Errorf("parseIconSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestResolveHref(t *testing.T) {
	base, _ := url.Parse("https://example.com/dir/page")
	for _, tc := range []struct {
		href, want string
	}{
		{"/root.png", "https://example.com/root.png"},
		{"rel.png", "https://example.com/dir/rel.png"},
		{"https://cdn.example.com/x.png", "https://cdn.example.com/x.png"},
		{"data:image/png;base64,AAAA", ""}, // non-http dropped
		{"javascript:alert(1)", ""},
	} {
		if got := resolveHref(base, tc.href); got != tc.want {
			t.Errorf("resolveHref(%q) = %q, want %q", tc.href, got, tc.want)
		}
	}
}

func TestIsSVGIcon(t *testing.T) {
	if !isSVGIcon("https://x.com/i.svg", "") {
		t.Error("path .svg should be detected")
	}
	if !isSVGIcon("https://x.com/i", "image/svg+xml") {
		t.Error("type image/svg+xml should be detected")
	}
	if isSVGIcon("https://x.com/i.png", "image/png") {
		t.Error("png must not be flagged as svg")
	}
}

func TestDedupeStrings(t *testing.T) {
	got := dedupeStrings([]string{"a", "", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedupeStrings = %v, want %v", got, want)
	}
}
