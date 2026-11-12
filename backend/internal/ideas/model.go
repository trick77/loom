// Package ideas keeps what a user wants to follow up later, always with the
// reason why, so it is still understandable weeks later.
package ideas

import "time"

// States. A proposal comes from the idea tools and becomes an idea only when
// the user confirms its card (Store.Confirm): a tool never persists a visible
// idea on its own.
const (
	StateProposed = "proposed"
	StateOpen     = "open"
	StateDone     = "done"
)

// Idea is one thing to follow up, with the reason why.
//
// Table: ideas
type Idea struct {
	ID     string `json:"id"`
	UserID string `json:"-"`
	State  string `json:"state"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
	// Excerpt is the captured passage: outside text. It reaches a model only
	// as a pasted block marked outside (chat.MessagePastedText.Outside),
	// which locks the turn that answers it.
	Excerpt string `json:"excerpt"`
	// Question is the user's message the captured answer replied to, taken
	// by the server on save without pasted blocks: the user's own words.
	Question  string  `json:"question"`
	ThreadID  *string `json:"threadId"`
	MessageID *string `json:"messageId"`
	// ThreadTitle is the thread's title when the idea was captured; it stays
	// after the thread is deleted (ThreadDeleted).
	ThreadTitle   string `json:"threadTitle"`
	ThreadDeleted bool   `json:"threadDeleted"`
	// DueOn and RemindOn are 'YYYY-MM-DD' civil dates in the user's zone.
	DueOn             *string    `json:"dueOn"`
	RemindOn          *string    `json:"remindOn"`
	RemindedAt        *time.Time `json:"-"`
	RemindDismissedAt *time.Time `json:"-"`
	DoneAt            *time.Time `json:"doneAt"`
	DeletedAt         *time.Time `json:"-"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// Input is a new idea or proposal. ThreadID and MessageID are optional; a
// MessageID needs its ThreadID.
//
// Table: -
type Input struct {
	Title     string
	Reason    string
	Excerpt   string
	ThreadID  string
	MessageID string
	DueOn     OptionalDate
	RemindOn  OptionalDate
}

// Patch changes an open or done idea; nil and unset fields stay.
//
// Table: -
type Patch struct {
	Title           *string
	Reason          *string
	Excerpt         *string
	DueOn           OptionalDate
	RemindOn        OptionalDate
	Done            *bool
	RemindDismissed *bool
}
