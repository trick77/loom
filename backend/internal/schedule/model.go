package schedule

import "time"

// Schedule is one prompt that runs on its own.
//
// Table: schedules
type Schedule struct {
	ID             string     `json:"id"`
	UserID         string     `json:"-"`
	Name           string     `json:"name"`
	Prompt         string     `json:"prompt"`
	IntervalText   string     `json:"interval"`
	CronTemplate   string     `json:"-"`
	Cron           string     `json:"cron"`
	Timezone       string     `json:"timezone"`
	Enabled        bool       `json:"enabled"`
	NextRunAt      *time.Time `json:"nextRunAt"`
	Running        bool       `json:"running"`
	DisabledReason string     `json:"disabledReason"`
	DisabledAt     *time.Time `json:"disabledAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// Run is one execution of a schedule.
//
// Table: schedule_runs
type Run struct {
	ID         string `json:"id"`
	ScheduleID string `json:"scheduleId"`
	// ThreadID is the thread the run started; nil once the user deleted it.
	ThreadID    *string    `json:"threadId"`
	MessageID   *string    `json:"messageId"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	Status      string     `json:"status"`
	Rounds      int        `json:"rounds"`
	Tokens      int64      `json:"tokens"`
	CostNanoUSD int64      `json:"-"`
	DurationMS  int64      `json:"durationMs"`
	Error       string     `json:"error"`
}

// InterruptedRun is the thread a run left behind when the process stopped.
//
// Table: schedule_runs
type InterruptedRun struct {
	UserID   string
	ThreadID string
}

// Source is what the runs learned about one page.
//
// Table: schedule_sources
type Source struct {
	URLKey        string     `json:"url"`
	Prefer        string     `json:"prefer"`
	FetchFailedAt *time.Time `json:"fetchFailedAt"`
	BrowserOKAt   *time.Time `json:"browserOkAt"`
	Runs          int        `json:"runs"`
	Finds         int        `json:"finds"`
	LastUsedAt    time.Time  `json:"lastUsedAt"`
}

// Query is a search a run issued and the finds it led to.
//
// Table: schedule_queries
type Query struct {
	Query      string    `json:"query"`
	Runs       int       `json:"runs"`
	Finds      int       `json:"finds"`
	EmptyRuns  int       `json:"emptyRuns"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

// Notification is one item in the user's inbox.
//
// Table: notifications
type Notification struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	ScheduleID *string    `json:"scheduleId"`
	ThreadID   *string    `json:"threadId"`
	MessageID  *string    `json:"messageId"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReadAt     *time.Time `json:"readAt"`
}
