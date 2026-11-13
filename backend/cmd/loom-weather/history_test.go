package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/toolkit/mcpserver"
)

func TestHistory(t *testing.T) {
	var archive, forecast atomic.Int32
	var lastQuery atomic.Value
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastQuery.Store(r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/archive":
			archive.Add(1)
		case "/v1/forecast":
			forecast.Add(1)
		default:
			http.NotFound(w, r)
			return
		}
		d := r.URL.Query().Get("start_date")
		_, _ = fmt.Fprintf(w, `{"latitude":47.38,"longitude":8.54,"timezone":"Europe/Zurich","utc_offset_seconds":3600,
			"hourly":{"time":["%sT07:00","%sT08:00"],"temperature_2m":[14.5,16.0],"apparent_temperature":[13.1,15.2],
			"relative_humidity_2m":[80,74]}}`, d, d)
	}))
	t.Cleanup(provider.Close)
	now := func() time.Time { return time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC) }
	w := &weather{provider: openMeteo{http: http.DefaultClient, forecastURL: provider.URL, archiveURL: provider.URL},
		history: newCache[History](now, cacheLimit), now: now}

	call := func(args string) mcpserver.Result { return w.runHistory(t.Context(), json.RawMessage(args)) }
	r := call(`{"latitude":47.3812,"longitude":8.5412,"date":"2026-03-01"}`)
	var h History
	if r.IsError || json.Unmarshal([]byte(r.Text), &h) != nil || len(h.Hourly) != 2 || h.Hourly[1].Temperature != 16 ||
		h.Timezone != "Europe/Zurich" || h.Attribution.URL == "" {
		t.Fatalf("archive day: %+v", r)
	}
	if archive.Load() != 1 || forecast.Load() != 0 || !strings.Contains(lastQuery.Load().(string), "latitude=47.38") {
		t.Errorf("an old day from the archive, the point rounded: archive %d forecast %d, %v", archive.Load(), forecast.Load(), lastQuery.Load())
	}
	// Kept: the past does not change.
	call(`{"latitude":47.3812,"longitude":8.5412,"date":"2026-03-01"}`)
	if archive.Load() != 1 {
		t.Error("an archived day read twice")
	}
	// A recent day: the forecast API's past hours.
	if r := call(`{"latitude":47.38,"longitude":8.54,"date":"2026-11-10"}`); r.IsError || forecast.Load() != 1 {
		t.Errorf("recent day: %+v, forecast %d", r, forecast.Load())
	}
	// Tomorrow by UTC is today in Auckland.
	if r := call(`{"latitude":-36.85,"longitude":174.76,"date":"2026-11-14"}`); r.IsError {
		t.Errorf("a day ahead of UTC refused: %s", r.Text)
	}
	for _, bad := range []string{`{"latitude":47,"longitude":8,"date":"2026-11-15"}`, `{"latitude":47,"longitude":8,"date":"1900-01-01"}`,
		`{"latitude":47,"longitude":8,"date":"yesterday"}`, `{"latitude":95,"longitude":8,"date":"2026-01-01"}`, `{"date":"2026-01-01"}`} {
		if r := call(bad); !r.IsError {
			t.Errorf("%s accepted", bad)
		}
	}
	if s := w.mcpServer("test"); !slicesHasTool(s, historyTool) || historyTool != mcp.WeatherHistoryTool {
		t.Error("history not offered under the contract's name")
	}
}

func slicesHasTool(s mcpserver.Server, name string) bool {
	for _, tl := range s.Tools {
		if tl.Name == name {
			return true
		}
	}
	return false
}
