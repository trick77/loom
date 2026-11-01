package chat

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// User memory is one row per fact (user_facts). A background refresh folds new
// messages in as a list of ops; a fact no op names is left untouched, so
// nothing is lost to a rewrite. Time is code's job: last_seen moves when a
// fact is mentioned again, buckets are computed from it, and a fact still
// being reinforced UserFactDurableSpan after it was first seen becomes
// long-term (durable).

// Fact categories: a topic only. Time is never a category.
const (
	FactCategoryWork     = "work"
	FactCategoryPersonal = "personal"
	FactCategoryInterest = "interest"
)

// FactCategories lists the valid categories in render order.
var FactCategories = []string{FactCategoryWork, FactCategoryPersonal, FactCategoryInterest}

// Fact origins. Only extracted facts depend on their source threads: deleting
// the last of them retires the fact. Tool-written and converted facts never do.
// Converted facts came from the old memory blob (0.2.14); none are made now.
const (
	FactOriginExtracted = "extracted"
	FactOriginTool      = "tool"
	FactOriginConverted = "converted"
)

// Retire reasons. Age is never one: old facts move to Earlier context and stay.
const (
	FactRetiredSuperseded    = "superseded"
	FactRetiredUser          = "user"
	FactRetiredThreadDeleted = "thread_deleted"
)

// Fact op kinds a refresh may emit.
const (
	FactOpAdd       = "add"
	FactOpUpdate    = "update"
	FactOpReinforce = "reinforce"
	FactOpRetire    = "retire"
)

const (
	// MaxUserFactLength caps one fact's content (runes). Tool writes over it are
	// refused; background ops over it are dropped.
	MaxUserFactLength = 500
	// UserFactBatchLimit caps how many messages one refresh batch reads. A
	// larger backlog is processed in further batches, never skipped.
	UserFactBatchLimit = 1000
	// UserFactDurableSpan promotes a fact to long-term once last_seen -
	// first_seen reaches it: still being reinforced after many months.
	UserFactDurableSpan = 180 * 24 * time.Hour
)

// ErrUserFactInvalid refuses a tool write with a bad category, blank content
// or content over MaxUserFactLength.
var ErrUserFactInvalid = errors.New("invalid user fact")

// UserFact is one active fact about the user.
type UserFact struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Durable   bool      `json:"durable"`
	Origin    string    `json:"-"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// UserFactOp is one change a refresh asks for. Ref names an added fact within
// the batch so a retire can point SupersededBy at it. Threads are the source
// threads the op came from; empty means every thread of the batch.
type UserFactOp struct {
	Kind         string
	Ref          string
	ID           string
	Category     string
	Content      string
	Durable      bool
	SupersededBy string
	Threads      []string
	// SeenAt, when set, is the op's own message date in place of the batch's:
	// the first_seen and last_seen of an add, the new last_seen of an update
	// or reinforce.
	SeenAt time.Time
}

// UserFactBatch is one refresh's ops plus the cursor it advances to. The ops,
// the long-term promotion, the orphan retire, the purge of forgotten facts and
// the cursor move commit together. Added facts are extracted facts.
type UserFactBatch struct {
	Ops []UserFactOp
	// SeenAt is the message date the batch stands for: first_seen of an add,
	// the new last_seen of an update or reinforce.
	SeenAt time.Time
	// Threads is the fallback source set for ops that name none.
	Threads []string
	// Cursor, when set, is stored as the new refresh cursor.
	Cursor *UserFactCursor
	// PurgeForgottenBefore, when set, hard-deletes the facts the user forgot
	// before that time: when the refresh read them as "do not re-add". A fact
	// forgotten later (during the model call) waits for the next refresh.
	PurgeForgottenBefore *time.Time
}

// UserFactCursor is a user's refresh position: the last message folded in,
// ordered by (created_at, id), plus when the last refresh and consolidation ran.
type UserFactCursor struct {
	LastCreatedAt      string
	LastMessageID      string
	LastRefreshAt      *time.Time
	LastConsolidatedAt *time.Time
}

// UserFactApplyResult counts what a batch did.
type UserFactApplyResult struct {
	Added, Updated, Reinforced, Retired, Dropped int
}

// UserFactBucket names a fact's time bucket.
type UserFactBucket string

// Time buckets, in render order.
const (
	BucketTopOfMind UserFactBucket = "top_of_mind"
	BucketRecent    UserFactBucket = "recent"
	BucketEarlier   UserFactBucket = "earlier"
	BucketLongTerm  UserFactBucket = "long_term"
)

// UserFactBuckets lists the buckets in render order.
var UserFactBuckets = []UserFactBucket{BucketLongTerm, BucketTopOfMind, BucketRecent, BucketEarlier}

const (
	topOfMindAge = 30 * 24 * time.Hour
	recentAge    = 180 * 24 * time.Hour
)

// Bucket computes a fact's time bucket at now: long-term when durable,
// otherwise by how long ago it was last seen.
func (f UserFact) Bucket(now time.Time) UserFactBucket {
	if f.Durable {
		return BucketLongTerm
	}
	age := now.Sub(f.LastSeen)
	switch {
	case age < topOfMindAge:
		return BucketTopOfMind
	case age < recentAge:
		return BucketRecent
	default:
		return BucketEarlier
	}
}

// SortUserFacts orders facts bucket first (UserFactBuckets order), then
// category (FactCategories order), newest last_seen first. Render and UI share
// it.
func SortUserFacts(facts []UserFact, now time.Time) {
	bucketRank := func(f UserFact) int { return slices.Index(UserFactBuckets, f.Bucket(now)) }
	categoryRank := func(f UserFact) int { return slices.Index(FactCategories, f.Category) }
	slices.SortStableFunc(facts, func(a, b UserFact) int {
		if d := bucketRank(a) - bucketRank(b); d != 0 {
			return d
		}
		if d := categoryRank(a) - categoryRank(b); d != 0 {
			return d
		}
		return b.LastSeen.Compare(a.LastSeen)
	})
}

// validFactContent trims content and reports whether it is a storable fact.
func validFactContent(content string) (string, bool) {
	content = strings.TrimSpace(content)
	return content, content != "" && len([]rune(content)) <= MaxUserFactLength
}
