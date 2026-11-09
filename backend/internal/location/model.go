package location

// The kinds of place a user has.
const (
	KindHome    = "home"
	KindCurrent = "current"
)

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
