package location

// The kinds of place a user has.
const (
	KindHome    = "home"
	KindCurrent = "current"
	// KindLocated is the town the browser's position named.
	KindLocated = "located"
)

// Located is the town the browser's position named, set by the user on the
// start page. It holds over the places their memory names until they locate
// again or drop it, and is never a fact: the memory may be stale or wrong, the
// position is where the user is.
//
// Table: user_located_places
type Located struct {
	Place
	// Credit names the map data the town's name came from, shown with it.
	Credit Credit `json:"credit"`
}

// Credit is the map data a located town's name came from.
//
// Table: -
type Credit struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// Place is a town's centre: where the user lives, or where they are for a
// while.
//
// Table: user_locations
type Place struct {
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	Region    string  `json:"region,omitempty"`
	Country   string  `json:"country,omitempty"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone,omitempty"`
	// Since and Until are the first and last day (YYYY-MM-DD, the user's
	// zone) a current place holds; "" for home, or an open end.
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	// FactID is the fact the place was read from, Query the name and country
	// code it was looked up by.
	FactID string `json:"-"`
	Query  string `json:"-"`
}
