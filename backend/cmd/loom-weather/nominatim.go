package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/ship/kit/ratelimit"
)

// nominatim is OpenStreetMap's Nominatim reverse lookup: the town a point
// lies in. The weather provider has no reverse lookup. The public server's
// usage policy (operations.osmfoundation.org/policies/nominatim): at most one
// request a second from the whole application, an agent naming it, answers
// cached, the ODbL credit shown with the data.
type nominatim struct {
	http      *http.Client
	url       string
	userAgent string
	// limit spaces the calls reverseInterval apart, refusing past
	// maxReverseWait.
	limit *ratelimit.Throttle
}

// reverseAttribution is the credit the map data's licence (ODbL) asks for.
var reverseAttribution = Attribution{Text: "Place names © OpenStreetMap contributors", URL: "https://www.openstreetmap.org/copyright"}

// reverseInterval spaces the calls to the map provider.
var reverseInterval = time.Second

// maxReverseWait bounds the queue for the map provider: a caller whose turn
// lies further out is refused at once, not queued. The calls run detached
// from their callers (the cache folds them), so an unbounded queue would
// keep spending the provider's allowance minutes after the callers left.
const maxReverseWait = 5 * time.Second

// errMapBusy is a reverse lookup refused for the queue.
var errMapBusy = errors.New("map provider busy, try again shortly")

// townZoom asks the reverse lookup down to a village or suburb, never a
// street or house: 10 stops at the municipality, so a village without a
// boundary of its own (an English parish) came back as its district.
const townZoom = "13"

// townKeys are the address parts naming a town, narrowest first: a village
// the weather search knows by its own name is that village. Hamlet comes
// last: a place a weather search may still know when nothing else names the
// point. Never the county: a town of its name nearby is no town the point
// lies in.
var townKeys = []string{"village", "town", "city", "municipality", "hamlet"}

// town is what the map provider calls the place at a point.
type town struct {
	// names are the place's names, narrowest first, without repeats.
	names       []string
	countryCode string
}

// reverse names the town at a point; a point without one (the sea) has no
// names.
func (n nominatim) reverse(ctx context.Context, lat, lon float64, language string) (town, error) {
	// One key: the policy's limit is the whole sidecar's.
	if err := n.limit.Wait(ctx, ""); err != nil {
		if errors.Is(err, ratelimit.ErrThrottled) {
			return town{}, errMapBusy
		}
		return town{}, err
	}
	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("zoom", townZoom)
	q.Set("format", "jsonv2")
	q.Set("addressdetails", "1")
	q.Set("accept-language", language)
	var raw struct {
		Address map[string]string `json:"address"`
	}
	if err := fetchJSON(ctx, n.http, "map provider", n.url+"/reverse?"+q.Encode(), n.userAgent, &raw); err != nil {
		return town{}, err
	}
	var t town
	for _, key := range townKeys {
		if name := strings.TrimSpace(raw.Address[key]); name != "" && !slices.Contains(t.names, name) {
			t.names = append(t.names, name)
		}
	}
	t.countryCode = strings.ToUpper(strings.TrimSpace(raw.Address["country_code"]))
	if !countryPattern.MatchString(t.countryCode) {
		t.countryCode = ""
	}
	return t, nil
}
