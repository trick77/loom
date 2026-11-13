package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The parts of Strava's API v3 models the tools read. Everything about other
// athletes (kudoers, comments, clubs, leaderboards) is left out on purpose.

type athlete struct {
	ID                    int64         `json:"id"`
	Firstname             string        `json:"firstname"`
	Lastname              string        `json:"lastname"`
	City                  string        `json:"city"`
	Country               string        `json:"country"`
	Sex                   string        `json:"sex"`
	Summit                bool          `json:"summit"`
	MeasurementPreference string        `json:"measurement_preference"` // "meters" or "feet"
	FTP                   float64       `json:"ftp"`
	Weight                float64       `json:"weight"` // kg
	Bikes                 []summaryGear `json:"bikes"`
	Shoes                 []summaryGear `json:"shoes"`
}

func (a athlete) name() string { return strings.TrimSpace(a.Firstname + " " + a.Lastname) }

type summaryGear struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Primary  bool    `json:"primary"`
	Retired  bool    `json:"retired"`
	Distance float64 `json:"distance"` // m
}

type zoneRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"` // -1: open-ended
}

type athleteZones struct {
	HeartRate *struct {
		CustomZones bool        `json:"custom_zones"`
		Zones       []zoneRange `json:"zones"`
	} `json:"heart_rate"`
	Power *struct {
		Zones []zoneRange `json:"zones"`
	} `json:"power"`
}

type totals struct {
	Count         int     `json:"count"`
	Distance      float64 `json:"distance"`
	MovingTime    float64 `json:"moving_time"`
	ElapsedTime   float64 `json:"elapsed_time"`
	ElevationGain float64 `json:"elevation_gain"`
}

type athleteStats struct {
	BiggestRideDistance       float64 `json:"biggest_ride_distance"`
	BiggestClimbElevationGain float64 `json:"biggest_climb_elevation_gain"`
	RecentRide                totals  `json:"recent_ride_totals"`
	RecentRun                 totals  `json:"recent_run_totals"`
	RecentSwim                totals  `json:"recent_swim_totals"`
	YTDRide                   totals  `json:"ytd_ride_totals"`
	YTDRun                    totals  `json:"ytd_run_totals"`
	YTDSwim                   totals  `json:"ytd_swim_totals"`
	AllRide                   totals  `json:"all_ride_totals"`
	AllRun                    totals  `json:"all_run_totals"`
	AllSwim                   totals  `json:"all_swim_totals"`
}

// latlng is Strava's [lat, lng]; empty when the activity has no GPS.
type latlng []float64

func (l latlng) ok() bool { return len(l) == 2 && (l[0] != 0 || l[1] != 0) }

type summaryActivity struct {
	ID                   int64              `json:"id"`
	Athlete              struct{ ID int64 } `json:"athlete"`
	Name                 string             `json:"name"`
	SportType            string             `json:"sport_type"`
	Type                 string             `json:"type"`
	StartDate            string             `json:"start_date"`
	StartDateLocal       string             `json:"start_date_local"`
	Distance             float64            `json:"distance"`
	MovingTime           int                `json:"moving_time"`
	ElapsedTime          int                `json:"elapsed_time"`
	TotalElevationGain   float64            `json:"total_elevation_gain"`
	AverageSpeed         float64            `json:"average_speed"`
	MaxSpeed             float64            `json:"max_speed"`
	HasHeartrate         bool               `json:"has_heartrate"`
	AverageHeartrate     float64            `json:"average_heartrate"`
	MaxHeartrate         float64            `json:"max_heartrate"`
	AverageWatts         float64            `json:"average_watts"`
	WeightedAverageWatts float64            `json:"weighted_average_watts"`
	MaxWatts             float64            `json:"max_watts"`
	DeviceWatts          bool               `json:"device_watts"`
	Kilojoules           float64            `json:"kilojoules"`
	AverageCadence       float64            `json:"average_cadence"`
	AverageTemp          float64            `json:"average_temp"`
	Trainer              bool               `json:"trainer"`
	Commute              bool               `json:"commute"`
	Manual               bool               `json:"manual"`
	Private              bool               `json:"private"`
	SufferScore          float64            `json:"suffer_score"`
	PRCount              int                `json:"pr_count"`
	AchievementCount     int                `json:"achievement_count"`
	KudosCount           int                `json:"kudos_count"`
	CommentCount         int                `json:"comment_count"`
	GearID               string             `json:"gear_id"`
	StartLatLng          latlng             `json:"start_latlng"`
	EndLatLng            latlng             `json:"end_latlng"`
}

// localStart is the activity's start in the athlete's wall clock. Strava
// writes start_date_local with a Z although it is local time: the zone is
// dropped and the clock kept.
func (a summaryActivity) localStart() time.Time {
	t, err := time.Parse(time.RFC3339, a.StartDateLocal)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, a.StartDate)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

func (a summaryActivity) utcStart() time.Time {
	t, err := time.Parse(time.RFC3339, a.StartDate)
	if err != nil {
		return a.localStart()
	}
	return t.UTC()
}

// indoor: Zwift and other virtual sports, or a trainer ride.
func (a summaryActivity) indoor() bool { return a.Trainer || strings.HasPrefix(a.SportType, "Virtual") }

type lap struct {
	Name               string  `json:"name"`
	LapIndex           int     `json:"lap_index"`
	ElapsedTime        int     `json:"elapsed_time"`
	MovingTime         int     `json:"moving_time"`
	Distance           float64 `json:"distance"`
	StartIndex         int     `json:"start_index"`
	EndIndex           int     `json:"end_index"`
	TotalElevationGain float64 `json:"total_elevation_gain"`
	AverageSpeed       float64 `json:"average_speed"`
	AverageWatts       float64 `json:"average_watts"`
	AverageHeartrate   float64 `json:"average_heartrate"`
	MaxHeartrate       float64 `json:"max_heartrate"`
	AverageCadence     float64 `json:"average_cadence"`
}

type split struct {
	Split               int     `json:"split"`
	Distance            float64 `json:"distance"`
	ElapsedTime         int     `json:"elapsed_time"`
	MovingTime          int     `json:"moving_time"`
	ElevationDifference float64 `json:"elevation_difference"`
	AverageSpeed        float64 `json:"average_speed"`
	AverageHeartrate    float64 `json:"average_heartrate"`
	// AverageGradeAdjustedSpeed is Strava's grade-adjusted pace (runs only).
	AverageGradeAdjustedSpeed float64 `json:"average_grade_adjusted_speed"`
}

type bestEffort struct {
	Name        string  `json:"name"`
	ElapsedTime int     `json:"elapsed_time"`
	Distance    float64 `json:"distance"`
	PRRank      int     `json:"pr_rank"`
}

type segmentEffort struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	ElapsedTime      int     `json:"elapsed_time"`
	MovingTime       int     `json:"moving_time"`
	Distance         float64 `json:"distance"`
	StartDateLocal   string  `json:"start_date_local"`
	AverageWatts     float64 `json:"average_watts"`
	AverageHeartrate float64 `json:"average_heartrate"`
	MaxHeartrate     float64 `json:"max_heartrate"`
	PRRank           int     `json:"pr_rank"`
	KOMRank          int     `json:"kom_rank"`
	Activity         struct {
		ID int64 `json:"id"`
	} `json:"activity"`
	Segment struct {
		ID            int64   `json:"id"`
		Name          string  `json:"name"`
		AverageGrade  float64 `json:"average_grade"`
		ClimbCategory int     `json:"climb_category"`
	} `json:"segment"`
}

type detailedActivity struct {
	summaryActivity
	Description       string  `json:"description"`
	PrivateNote       string  `json:"private_note"`
	DeviceName        string  `json:"device_name"`
	Calories          float64 `json:"calories"`
	PerceivedExertion float64 `json:"perceived_exertion"`
	WorkoutType       *int    `json:"workout_type"`
	ElevHigh          float64 `json:"elev_high"`
	ElevLow           float64 `json:"elev_low"`
	Gear              *struct {
		ID       string  `json:"id"`
		Name     string  `json:"name"`
		Distance float64 `json:"distance"`
	} `json:"gear"`
	Laps           []lap           `json:"laps"`
	SplitsMetric   []split         `json:"splits_metric"`
	SplitsStandard []split         `json:"splits_standard"`
	BestEfforts    []bestEffort    `json:"best_efforts"`
	SegmentEfforts []segmentEffort `json:"segment_efforts"`
	Photos         struct {
		Count int `json:"count"`
	} `json:"photos"`
	LocationCity    string `json:"location_city"`
	LocationCountry string `json:"location_country"`
}

type zoneBucket struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Time float64 `json:"time"`
}

type activityZone struct {
	Type    string       `json:"type"` // heartrate, power
	Buckets []zoneBucket `json:"distribution_buckets"`
}

type gearDetail struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	BrandName   string  `json:"brand_name"`
	ModelName   string  `json:"model_name"`
	FrameType   int     `json:"frame_type"`
	Description string  `json:"description"`
	Distance    float64 `json:"distance"`
	Primary     bool    `json:"primary"`
	Retired     bool    `json:"retired"`
}

type summarySegment struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	ActivityType  string  `json:"activity_type"`
	Distance      float64 `json:"distance"`
	AverageGrade  float64 `json:"average_grade"`
	MaximumGrade  float64 `json:"maximum_grade"`
	ElevationHigh float64 `json:"elevation_high"`
	ElevationLow  float64 `json:"elevation_low"`
	ClimbCategory int     `json:"climb_category"`
	City          string  `json:"city"`
	Country       string  `json:"country"`
	PR            *struct {
		ActivityID  int64  `json:"pr_activity_id"`
		ElapsedTime int    `json:"pr_elapsed_time"`
		Date        string `json:"pr_date"`
		EffortCount int    `json:"effort_count"`
	} `json:"athlete_pr_effort"`
	Stats *struct {
		ElapsedTime int    `json:"pr_elapsed_time"`
		Date        string `json:"pr_date"`
		EffortCount int    `json:"effort_count"`
	} `json:"athlete_segment_stats"`
}

type route struct {
	ID                  int64            `json:"id"`
	IDStr               string           `json:"id_str"`
	Name                string           `json:"name"`
	Description         string           `json:"description"`
	Distance            float64          `json:"distance"`
	ElevationGain       float64          `json:"elevation_gain"`
	Type                int              `json:"type"` // 1 ride, 2 run
	EstimatedMovingTime float64          `json:"estimated_moving_time"`
	CreatedAt           string           `json:"created_at"`
	Private             bool             `json:"private"`
	Starred             bool             `json:"starred"`
	Segments            []summarySegment `json:"segments"`
}

func (r route) idString() string {
	if r.IDStr != "" {
		return r.IDStr
	}
	return strconv.FormatInt(r.ID, 10)
}

// floats is a numeric stream that tolerates nulls (a power dropout): they
// become NaN, filled when the series is built.
type floats []float64

func (f *floats) UnmarshalJSON(b []byte) error {
	var raw []*float64
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	out := make(floats, len(raw))
	for i, v := range raw {
		if v == nil {
			out[i] = math.NaN()
		} else {
			out[i] = *v
		}
	}
	*f = out
	return nil
}

// streamSet is /activities/{id}/streams with key_by_type=true.
type streamSet struct {
	Time     *struct{ Data floats }      `json:"time"`
	Distance *struct{ Data floats }      `json:"distance"`
	LatLng   *struct{ Data [][]float64 } `json:"latlng"`
	Altitude *struct{ Data floats }      `json:"altitude"`
	Velocity *struct{ Data floats }      `json:"velocity_smooth"`
	HR       *struct{ Data floats }      `json:"heartrate"`
	Cadence  *struct{ Data floats }      `json:"cadence"`
	Watts    *struct{ Data floats }      `json:"watts"`
	Temp     *struct{ Data floats }      `json:"temp"`
	Moving   *struct{ Data []bool }      `json:"moving"`
	Grade    *struct{ Data floats }      `json:"grade_smooth"`
}

const streamKeys = "time,distance,latlng,altitude,velocity_smooth,heartrate,cadence,watts,temp,moving,grade_smooth"

// --- cached reads, per user ---

func (s *service) athlete(ctx context.Context, user string) (athlete, error) {
	return cached[athlete](ctx, s, user, s.cache(user).profile, "athlete", "/athlete", nil)
}

func (s *service) zones(ctx context.Context, user string) (athleteZones, error) {
	return cached[athleteZones](ctx, s, user, s.cache(user).profile, "zones", "/athlete/zones", nil)
}

func (s *service) stats(ctx context.Context, user string, athleteID int64) (athleteStats, error) {
	return cached[athleteStats](ctx, s, user, s.cache(user).profile, "stats", fmt.Sprintf("/athletes/%d/stats", athleteID), nil)
}

func (s *service) gear(ctx context.Context, user, id string) (gearDetail, error) {
	return cached[gearDetail](ctx, s, user, s.cache(user).profile, "gear/"+id, "/gear/"+url.PathEscape(id), nil)
}

// storedRead reads one activity's answer of kind: memory c, the store,
// then Strava (always Strava with refresh). keep sees what Strava answered,
// with the athlete the read was made for, and says whether to store it; it
// then replaces what was kept.
func storedRead[T any](ctx context.Context, s *service, user string, id int64, kind string, c *lru, refresh bool,
	path string, q url.Values, decode func([]byte) (T, error), keep func(v T, athlete int64) bool) (T, error) {
	var zero T
	key := kind + "/" + strconv.FormatInt(id, 10)
	if !refresh {
		if v, ok := c.get(key, s.now()); ok {
			if t, ok := v.(T); ok {
				return t, nil
			}
		}
		if b, ok := s.loadBody(user, id, kind); ok {
			if v, err := decode(b); err == nil {
				c.put(key, v, s.now())
				return v, nil
			}
		}
	}
	athlete := s.athleteOf(user)
	b, err := s.getRaw(ctx, user, path, q)
	if err != nil {
		return zero, err
	}
	v, err := decode(b)
	if err != nil {
		return zero, fmt.Errorf("strava %s: %w", path, err)
	}
	c.put(key, v, s.now())
	if keep == nil || keep(v, athlete) {
		s.keepBody(user, athlete, id, kind, b)
	} else if refresh {
		// The older copy must not come back once memory forgets this one.
		s.discardBody(user, id, kind)
	}
	return v, nil
}

func decodeJSON[T any](b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// activity reads one activity's detail through the store. A detail of the
// user's own is stored once settled and its summary goes into the list;
// Strava no longer having it drops what was kept.
func (s *service) activity(ctx context.Context, user string, id int64, refresh bool) (detailedActivity, error) {
	d, err := storedRead(ctx, s, user, id, kindDetail, s.cache(user).detail, refresh,
		fmt.Sprintf("/activities/%d", id), url.Values{"include_all_efforts": {"true"}}, decodeJSON[detailedActivity],
		func(d detailedActivity, athlete int64) bool {
			if athlete != 0 && d.Athlete.ID != 0 && d.Athlete.ID != athlete {
				return false
			}
			ok := s.settled(user, d.summaryActivity)
			s.noteActivity(user, athlete, d.summaryActivity)
			return ok
		})
	if notFound(err) {
		s.dropActivity(user, id)
	}
	return d, err
}

// activityZones is Strava's own time in zones (Summit only), through the
// store, with summitOnly's day-long refusal.
func (s *service) activityZones(ctx context.Context, user string, id int64, refresh bool) ([]activityZone, error) {
	uc := s.cache(user)
	if v, ok := uc.detail.get(refusedKey("zones"), s.now()); ok {
		return nil, v.(error)
	}
	z, err := storedRead(ctx, s, user, id, kindZones, uc.detail, refresh,
		fmt.Sprintf("/activities/%d/zones", id), nil, decodeJSON[[]activityZone], nil)
	if summitRefused(err) {
		uc.detail.put(refusedKey("zones"), err, s.now())
	}
	return z, err
}

func refusedKey(endpoint string) string { return "summit-refused/" + endpoint }

// summitOnly reads a Summit-only path through values, and keeps a refusal
// in refusals (the day-long detail cache), keyed by the endpoint alone: a
// user without a subscription is refused for every activity and segment,
// so one refused read a day is enough.
func summitOnly[T any](ctx context.Context, s *service, user string, values, refusals *lru, endpoint, key, path string, q url.Values) (T, error) {
	if v, ok := refusals.get(refusedKey(endpoint), s.now()); ok {
		var zero T
		return zero, v.(error)
	}
	out, err := cached[T](ctx, s, user, values, key, path, q)
	if summitRefused(err) {
		refusals.put(refusedKey(endpoint), err, s.now())
	}
	return out, err
}

// streams reads one activity's streams through the store. Streams are final
// at upload, so every answer is stored at once. An activity without any (a
// manual entry) answers 404, which is an empty set here and not stored.
func (s *service) streams(ctx context.Context, user string, id int64, refresh bool) (series, error) {
	c := s.cache(user).streams
	sr, err := storedRead(ctx, s, user, id, kindStreams, c, refresh,
		fmt.Sprintf("/activities/%d/streams", id), url.Values{"keys": {streamKeys}, "key_by_type": {"true"}},
		decodeStreams, nil)
	if notFound(err) {
		sr = buildSeries(streamSet{})
		c.put(kindStreams+"/"+strconv.FormatInt(id, 10), sr, s.now())
		return sr, nil
	}
	return sr, err
}

// decodeStreams is a streams answer as a series.
func decodeStreams(b []byte) (series, error) {
	var set streamSet
	if err := json.Unmarshal(b, &set); err != nil {
		return series{}, err
	}
	return buildSeries(set), nil
}

// streamsFromDisk decodes id's kept streams past both caches: a sweep over
// many activities (the grade profile) must neither push the ones in use out
// of memory nor mark itself read in the disk index (its least-recently-read
// eviction would then drop the streams the user does read). ok false when
// none are kept.
func (s *service) streamsFromDisk(user string, id int64) (series, bool) {
	b, ok := s.store.readBody(user, id, kindStreams)
	if !ok {
		return series{}, false
	}
	sr, err := decodeStreams(b)
	return sr, err == nil
}

// activitiesPerPage is Strava's maximum.
const activitiesPerPage = 200
