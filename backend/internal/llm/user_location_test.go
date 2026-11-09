package llm

import (
	"context"
	"strings"
	"testing"
)

func TestResolveUserLocation(t *testing.T) {
	var body string
	srv := intentServer(t, `{"home":{"place":" Zürich ","country":"ch","fact":"f1"},"current":{"place":"Lisbon","country":"PT","until":"2026-11-15","fact":"f2"}}`, &body)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	facts := []FactInput{{ID: "f1", Category: "personal", Content: "Lives Zurich", LastSeen: "2026-10-01"},
		{ID: "f2", Category: "personal", Content: "In Lisbon this week", LastSeen: "2026-11-10"}}
	got, err := c.ResolveUserLocation(context.Background(), facts, "2026-11-11")
	if err != nil {
		t.Fatal(err)
	}
	if got.Home == nil || *got.Home != (PlaceRef{Place: "Zürich", Country: "CH", FactID: "f1"}) {
		t.Fatalf("home = %+v", got.Home)
	}
	if got.Current == nil || got.Current.Until != "2026-11-15" || got.Current.Place != "Lisbon" {
		t.Fatalf("current = %+v", got.Current)
	}
	for _, part := range []string{"Today: 2026-11-11", "In Lisbon this week", "2026-11-10"} {
		if !strings.Contains(body, part) {
			t.Fatalf("request lacks %q", part)
		}
	}
}

func TestParseUserLocationDropsWhatItCannotTrust(t *testing.T) {
	facts := []FactInput{{ID: "f1"}, {ID: "f2"}}
	for reply, want := range map[string]string{
		`{"home":null,"current":null}`:                                                         "none",
		`{"home":{"place":"","fact":"f1"}}`:                                                    "none",
		`{"home":{"place":"Bern","fact":"f9"}}`:                                                "none",
		`{"home":{"place":"Bern","country":"Switzerland","fact":"f1","until":"x"}}`:            "home",
		`{"current":{"place":"Lisbon","fact":"f2"}}`:                                           "none",
		`{"current":{"place":"Lisbon","fact":"f2","until":"2026-13-45"}}`:                      "none",
		`{"current":{"place":"Lisbon","fact":"f2","until":"2026-11-15"}}`:                      "current",
		`{"current":{"place":"Lisbon","fact":"f2","since":"2026-11-20","until":"2026-11-26"}}`: "current",
		`{"current":{"place":"Lisbon","fact":"f2","since":"2026-11-20","until":"2026-11-15"}}`: "none",
		`{"current":{"place":"Lisbon","fact":"f2","since":"soon","until":"2026-11-15"}}`:       "none",
		"```json\n{\"home\":{\"place\":\"Bern\",\"fact\":\"f1\"}}\n```":                        "home",
		`{"home":{"place":"` + strings.Repeat("x", 101) + `","fact":"f1"}}`:                    "none",
	} {
		loc, ok := parseUserLocation(reply, facts)
		if !ok {
			t.Errorf("%s: unreadable", reply)
			continue
		}
		got := "none"
		switch {
		case loc.Home != nil:
			got = "home"
			if loc.Home.Until != "" || loc.Home.Country != "" {
				t.Errorf("%s: home = %+v, want no until and no bad country", reply, loc.Home)
			}
		case loc.Current != nil:
			got = "current"
		}
		if got != want {
			t.Errorf("%s: got %s, want %s", reply, got, want)
		}
	}
	for _, bad := range []string{"no json", `{"home":5}`} {
		if _, ok := parseUserLocation(bad, facts); ok {
			t.Errorf("%q read as a location", bad)
		}
	}
}

func TestResolveUserLocationUnreadable(t *testing.T) {
	srv := intentServer(t, "I think they live in Zurich.", nil)
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	if _, err := c.ResolveUserLocation(context.Background(), nil, "2026-11-11"); err == nil {
		t.Fatal("an unreadable reply gave no error")
	}
}
