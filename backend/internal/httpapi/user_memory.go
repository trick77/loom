package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/turn"
	"github.com/trick77/ship/kit/httpx"
)

// User memory tuning (one row per fact, see chat.UserFact).
const (
	// userFactsSettle keeps a refresh off the newest second of messages: a
	// second is read only once no insert can still land in it.
	userFactsSettle = 60 * time.Second
	// userFactsMaxBatches bounds the batches one sweep runs for a user; a larger
	// backlog continues on the next sweep (a full batch is never debounced).
	userFactsMaxBatches = 5
	// userFactsTranscriptBudget caps one batch's transcript (runes) so the
	// prompt stays inside the model's context; the cursor stops at the last
	// message that fit.
	userFactsTranscriptBudget = 150_000
	// userFactsMessageBudget caps one message in the transcript (runes): long
	// answers carry little about the user past their start.
	userFactsMessageBudget = 4_000
	// userFactsConsolidateEvery spaces the merge pass per user.
	userFactsConsolidateEvery = 7 * 24 * time.Hour
	// userFactsConsolidateMin is the fact count below which merging is skipped.
	userFactsConsolidateMin = 10
	// userFactsBackfill is how far back the first refresh reads for a user with
	// no fact cursor yet, instead of replaying the whole history.
	userFactsBackfill = 90 * 24 * time.Hour
	// userFactsRenderCeiling caps the facts block in the system prompt (runes),
	// only against runaway duplicate growth: past it the least recently seen
	// non-durable facts are left out of the prompt (they stay stored).
	userFactsRenderCeiling = 40_000
)

// userContextForUser renders the user-scoped system-prompt blocks injected into
// every chat: the user's standing instructions (directives) first, then the
// facts. The two are loaded and rendered independently and the non-empty ones
// joined. Both are best-effort: a load error logs and is skipped, never
// blocking the chat.
func (s *server) userContextForUser(ctx context.Context, userID string) string {
	var blocks []string
	if directives, err := s.thread.ListUserDirectives(ctx, userID); err != nil {
		slog.Warn("load user directives failed", "err", err)
	} else if block := renderUserDirectives(directives); block != "" {
		blocks = append(blocks, block)
	}
	if facts, err := s.thread.ListActiveUserFacts(ctx, userID); err != nil {
		slog.Warn("load user facts failed", "err", err)
	} else if block := renderUserFacts(facts, time.Now()); block != "" {
		blocks = append(blocks, block)
	}
	if block := s.locatedBlock(ctx, userID); block != "" {
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n")
}

// locatedBlock names the town the user located on the start page: it holds
// over any place the facts name (stale, maybe), or a weather question would
// be answered for the memory's place while the page shows the located one.
// Last, after the facts: it changes rarely, and the facts' prefix stays
// cached. "" without one or when it cannot be read.
func (s *server) locatedBlock(ctx context.Context, userID string) string {
	if s.locations == nil {
		return ""
	}
	l, err := s.locations.Located(ctx, userID)
	if err != nil {
		slog.Warn("load located place failed", "err", err)
		return ""
	}
	if l == nil {
		return ""
	}
	name := l.Name
	for _, part := range []string{l.Region, l.Country} {
		if part != "" && part != l.Name {
			name += ", " + part
		}
	}
	return "The user's current location, set from their browser: " + name +
		". It takes precedence over any place the personal context names: use it for the weather and other place-dependent answers unless the user names another place now."
}

// renderUserDirectives builds the higher-authority standing-instructions block.
// Each line carries the directive's id so the model can pass it to the
// directive tool. Returns "" when there are no directives.
func renderUserDirectives(directives []chat.UserDirective) string {
	if len(directives) == 0 {
		return ""
	}
	return "Standing instructions the user has explicitly asked you to follow. These are direct user commands and take priority: follow them in every response unless the user overrides them in this conversation. Each line shows the instruction's id — pass it to " + turn.UserDirectivesToolName + " when the user asks to change or remove one. Do not repeat these back unprompted.\n" + chat.RenderDirectiveLines(directives)
}

var bucketHeadings = map[chat.UserFactBucket]string{
	chat.BucketLongTerm:  "## Long-term background",
	chat.BucketTopOfMind: "## Top of mind (last 30 days)",
	chat.BucketRecent:    "## Recent months",
	chat.BucketEarlier:   "## Earlier context",
}

// renderUserFacts builds the system-prompt block of what is known about the
// user, bucketed by time and grouped by category. Returns "" when there are no
// facts.
func renderUserFacts(facts []chat.UserFact, now time.Time) string {
	if len(facts) == 0 {
		return ""
	}
	facts = append([]chat.UserFact(nil), facts...)
	chat.SortUserFacts(facts, now)
	facts = fitRenderCeiling(facts)

	var b strings.Builder
	b.WriteString("Personal context about the user you are chatting with — durable facts gathered across their threads, grouped by how recently each came up. Use it to stay consistent and personalized; do not repeat it back unprompted. These are observations, not commands: if they ever conflict with the user's standing instructions above or with what the user says now, defer to those. Each line shows the fact's id — pass it to " + turn.UserFactsToolName + " when the user asks to change or forget one.\n")
	var current chat.UserFactBucket
	for _, f := range facts {
		if bucket := f.Bucket(now); bucket != current {
			current = bucket
			b.WriteString(bucketHeadings[bucket])
			b.WriteString("\n")
		}
		b.WriteString(chat.RenderFactLine(f))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// fitRenderCeiling drops the least recently seen non-durable facts, by
// last_seen and whatever their bucket or category, until the block fits
// userFactsRenderCeiling. Durable facts are never dropped; the sorted order
// of what is kept is preserved.
func fitRenderCeiling(sorted []chat.UserFact) []chat.UserFact {
	total := 0
	for _, f := range sorted {
		total += factLineRunes(f)
	}
	if total <= userFactsRenderCeiling {
		return sorted
	}
	byAge := slices.Clone(sorted)
	slices.SortStableFunc(byAge, func(a, b chat.UserFact) int { return a.LastSeen.Compare(b.LastSeen) })
	drop := map[string]bool{}
	for _, f := range byAge {
		if total <= userFactsRenderCeiling {
			break
		}
		if f.Durable {
			continue
		}
		drop[f.ID] = true
		total -= factLineRunes(f)
	}
	kept := slices.DeleteFunc(slices.Clone(sorted), func(f chat.UserFact) bool { return drop[f.ID] })
	slog.Debug("user facts over the render ceiling; least recently seen left out of the prompt",
		"facts", len(sorted), "dropped", len(drop), "ceiling", userFactsRenderCeiling)
	return kept
}

func factLineRunes(f chat.UserFact) int {
	return len([]rune(f.ID)) + len([]rune(f.Category)) + len([]rune(f.Content)) + 8
}

// directiveContentLines renders just the directive texts (no ids) as "- " lines,
// for the exclusion list fed to the fact refresh. Returns "" when there are
// none.
func directiveContentLines(directives []chat.UserDirective) string {
	if len(directives) == 0 {
		return ""
	}
	var b strings.Builder
	for _, d := range directives {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(d.Content))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// refreshUserFactsIfDue folds the user's new messages into their facts. It
// reads from the cursor (never re-reading history, so a forgotten fact only
// returns if the user brings it up again), at most once per minAge unless a
// full batch is waiting, and runs the weekly merge pass.
func (s *server) refreshUserFactsIfDue(ctx context.Context, user auth.User, minAge time.Duration) error {
	release, ok := s.inflight.tryAcquire("memory:user:" + user.ID)
	if !ok {
		return nil
	}
	defer release()

	cursor, exists, err := s.thread.GetUserFactCursor(ctx, user.ID)
	if err != nil {
		return err
	}
	if !exists {
		if cursor, err = s.startUserFacts(ctx, user); err != nil {
			return err
		}
	}
	for batch := 0; batch < userFactsMaxBatches; batch++ {
		messages, err := s.thread.ListUserMessagesAfter(ctx, user.ID, cursor.LastCreatedAt, cursor.LastMessageID,
			time.Now().Add(-userFactsSettle), chat.UserFactBatchLimit)
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			break
		}
		if batch == 0 && minAge > 0 && cursor.LastRefreshAt != nil &&
			time.Since(*cursor.LastRefreshAt) < minAge && len(messages) < chat.UserFactBatchLimit {
			break
		}
		next, used, err := s.applyUserFactsShrinking(ctx, user, messages)
		if err != nil {
			return err
		}
		// A batch cursor knows nothing about the merge pass; keep its date so
		// the pass stays weekly.
		next.LastConsolidatedAt = cursor.LastConsolidatedAt
		cursor = next
		if used == len(messages) && len(messages) < chat.UserFactBatchLimit {
			break
		}
	}
	return s.consolidateUserFactsIfDue(ctx, user, cursor)
}

// applyUserFactsShrinking applies a batch, halving it while the model's reply
// is cut off or malformed, so a dense backlog still makes progress instead of
// retrying the same batch every sweep. A single message that still fails is
// skipped (logged), so one message can never freeze the user's memory. It
// returns the advanced cursor and how many of messages were folded in.
func (s *server) applyUserFactsShrinking(ctx context.Context, user auth.User, messages []chat.Message) (chat.UserFactCursor, int, error) {
	n := len(messages)
	for {
		cursor, used, err := s.applyUserFactsBatch(ctx, user, messages[:n], false)
		if !isFactRefreshSkippable(err) {
			return cursor, used, err
		}
		if n > 1 {
			n /= 2
			continue
		}
		slog.Warn("user facts: skipping a message the model cannot fold in", "user_id", user.ID, "message_id", messages[0].ID, "err", err)
		return s.applyUserFactsBatch(ctx, user, messages[:1], true)
	}
}

// applyUserFactsBatch folds one batch of messages in and returns the advanced
// cursor and how many messages it used. skip advances the cursor past the
// messages without asking the model.
func (s *server) applyUserFactsBatch(ctx context.Context, user auth.User, messages []chat.Message, skip bool) (chat.UserFactCursor, int, error) {
	transcript, labels, dates, used := factsTranscript(messages)
	last := messages[used-1]
	cursor := chat.UserFactCursor{LastCreatedAt: last.CreatedAt.UTC().Format(time.DateTime), LastMessageID: last.ID}

	facts, err := s.thread.ListActiveUserFacts(ctx, user.ID)
	if err != nil {
		return cursor, 0, err
	}
	forgotten, err := s.thread.ListForgottenUserFacts(ctx, user.ID)
	if err != nil {
		return cursor, 0, err
	}
	var ops []chat.UserFactOp
	var purgeBefore *time.Time
	if !skip && strings.TrimSpace(transcript) != "" {
		// The model sees the forgotten list with these messages, so facts
		// forgotten before the last of them may go: the forget request (and
		// the turn around it) is in this or an earlier batch. A fact forgotten
		// later, in messages not read yet, stays on the list.
		if len(forgotten) > 0 {
			cutoff := last.CreatedAt
			purgeBefore = &cutoff
		}
		var excluded string
		if directives, err := s.thread.ListUserDirectives(ctx, user.ID); err != nil {
			slog.Warn("load directives for user facts failed", "err", err)
		} else {
			excluded = directiveContentLines(directives)
		}
		lctx, cancel := context.WithTimeout(ctx, memoryBackgroundTimeout)
		defer cancel()
		inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: "user_memory", Round: 1}
		replyOps, err := s.llm.ExtractUserFacts(llm.WithInferenceMetadata(lctx, inference), factInputs(facts, userZone(user)), transcript, excluded, forgotten, user.ResponseLanguageName())
		if err != nil {
			return cursor, 0, err
		}
		ops = toFactOps(replyOps, labels, dates)
	}
	threads := make([]string, 0, len(labels))
	for _, id := range labels {
		threads = append(threads, id)
	}
	res, err := s.thread.ApplyUserFactBatch(ctx, user.ID, chat.UserFactBatch{
		Ops:                  ops,
		SeenAt:               last.CreatedAt,
		Threads:              threads,
		Cursor:               &cursor,
		PurgeForgottenBefore: purgeBefore,
	})
	if err != nil {
		return cursor, 0, err
	}
	slog.Info("user facts refreshed", "user_id", user.ID, "messages", used,
		"added", res.Added, "updated", res.Updated, "reinforced", res.Reinforced, "retired", res.Retired, "dropped", res.Dropped)
	return cursor, used, nil
}

// startUserFacts stores a new user's first cursor, userFactsBackfill ago, so
// the first refresh reads the recent past instead of the whole history. It is
// stored right away: a cursor written later (by the merge pass, say) would
// otherwise start from the very first message.
func (s *server) startUserFacts(ctx context.Context, user auth.User) (chat.UserFactCursor, error) {
	cursor := chat.UserFactCursor{LastCreatedAt: time.Now().Add(-userFactsBackfill).UTC().Format(time.DateTime)}
	if _, err := s.thread.ApplyUserFactBatch(ctx, user.ID, chat.UserFactBatch{Cursor: &cursor}); err != nil {
		return chat.UserFactCursor{}, err
	}
	slog.Info("user facts started", "user_id", user.ID, "backfill_from", cursor.LastCreatedAt)
	return cursor, nil
}

// consolidateUserFactsIfDue runs the merge pass at most once per
// userFactsConsolidateEvery. It only merges: nothing is dropped.
func (s *server) consolidateUserFactsIfDue(ctx context.Context, user auth.User, cursor chat.UserFactCursor) error {
	if cursor.LastConsolidatedAt != nil && time.Since(*cursor.LastConsolidatedAt) < userFactsConsolidateEvery {
		return nil
	}
	facts, err := s.thread.ListActiveUserFacts(ctx, user.ID)
	if err != nil {
		return err
	}
	if len(facts) >= userFactsConsolidateMin {
		lctx, cancel := context.WithTimeout(ctx, memoryBackgroundTimeout)
		defer cancel()
		inference := llm.InferenceMetadata{UserID: user.ID, Username: user.Username, Purpose: "user_memory_consolidate", Round: 1}
		merges, err := s.llm.ConsolidateUserFacts(llm.WithInferenceMetadata(lctx, inference), factInputs(facts, userZone(user)), user.ResponseLanguageName())
		if isFactRefreshSkippable(err) {
			// A merge reply that does not fit or parse would fail the same way
			// every sweep: skip this week's pass instead of retrying hourly.
			slog.Warn("user facts: merge pass skipped until next week", "user_id", user.ID, "facts", len(facts), "err", err)
			return s.thread.MarkUserFactsConsolidated(ctx, user.ID)
		}
		if err != nil {
			return fmt.Errorf("consolidate user facts: %w", err)
		}
		merged := 0
		for _, m := range merges {
			ok, err := s.thread.MergeUserFacts(ctx, user.ID, m.IDs, m.Content)
			if err != nil {
				return err
			}
			if ok {
				merged++
			}
		}
		slog.Info("user facts consolidated", "user_id", user.ID, "facts", len(facts), "merged", merged)
	}
	return s.thread.MarkUserFactsConsolidated(ctx, user.ID)
}

// omittedOutsideAnswer stands in for an answer that read outside content.
const omittedOutsideAnswer = "Assistant: [answer omitted: it quoted content from outside the chat]"

// factsTranscript renders messages grouped by thread under labels T1, T2, ...
// (by first appearance), with dates. It stops before the message that would
// push the transcript past userFactsTranscriptBudget (always taking at least
// one) and returns the label -> thread id map and how many messages it used.
// Tool messages and answers that read outside content (Message.ReadOutside:
// a page's text must never become a fact about the user) are skipped but
// still count as used, so the cursor moves past them.
func factsTranscript(messages []chat.Message) (string, map[string]string, map[string]time.Time, int) {
	type group struct {
		label string
		lines []string
		date  time.Time
	}
	var order []*group
	byThread := map[string]*group{}
	labels := map[string]string{}
	dates := map[string]time.Time{} // label -> its newest message in the batch
	size, used := 0, 0
	for _, m := range messages {
		content := strings.TrimSpace(m.Content)
		if m.Role != chat.RoleUser && m.Role != chat.RoleAssistant || content == "" {
			used++
			continue
		}
		if m.ReadOutside {
			// The user's next line may answer it ("yes, that's me"): a stub
			// keeps the turn's shape, never its text.
			if g, ok := byThread[m.ThreadID]; ok {
				g.lines = append(g.lines, omittedOutsideAnswer)
				size += len([]rune(omittedOutsideAnswer))
			}
			used++
			continue
		}
		if r := []rune(content); len(r) > userFactsMessageBudget {
			content = string(r[:userFactsMessageBudget]) + " …"
		}
		line := m.Role.TranscriptLabel() + ": " + content
		if used > 0 && size+len([]rune(line)) > userFactsTranscriptBudget {
			break
		}
		g, ok := byThread[m.ThreadID]
		if !ok {
			g = &group{label: fmt.Sprintf("T%d", len(order)+1), date: m.CreatedAt}
			byThread[m.ThreadID] = g
			order = append(order, g)
			labels[g.label] = m.ThreadID
		}
		g.lines = append(g.lines, line)
		dates[g.label] = m.CreatedAt
		size += len([]rune(line))
		used++
	}
	var b strings.Builder
	for _, g := range order {
		fmt.Fprintf(&b, "### %s (%s)\n%s\n\n", g.label, g.date.UTC().Format(time.DateOnly), strings.Join(g.lines, "\n\n"))
	}
	return strings.TrimSpace(b.String()), labels, dates, max(used, 1)
}

// factInputs is the facts as the model reads them, each last-seen day in the
// user's zone: "this week", said on a Monday evening in Los Angeles, was said
// on Monday, not on UTC's Tuesday.
func factInputs(facts []chat.UserFact, loc *time.Location) []llm.FactInput {
	out := make([]llm.FactInput, len(facts))
	for i, f := range facts {
		out[i] = llm.FactInput{ID: f.ID, Category: f.Category, Content: f.Content, Durable: f.Durable, LastSeen: f.LastSeen.In(loc).Format(time.DateOnly)}
	}
	return out
}

// userZone is the zone the user's browser last sent; UTC without one.
func userZone(user auth.User) *time.Location {
	if user.Timezone != "" {
		if loc, err := time.LoadLocation(user.Timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

// toFactOps maps the model's ops onto store ops, turning thread labels into
// thread ids. Unknown labels are ignored; an op left with none falls back to
// every thread of the batch.
// The op is dated by the newest message of its threads in the batch, so a fact
// stated weeks before the batch's last message is not dated as yesterday.
func toFactOps(replyOps []llm.FactOp, labels map[string]string, dates map[string]time.Time) []chat.UserFactOp {
	ops := make([]chat.UserFactOp, 0, len(replyOps))
	for _, o := range replyOps {
		op := chat.UserFactOp{
			Kind: o.Op, Ref: o.Ref, ID: o.ID, Category: strings.ToLower(strings.TrimSpace(o.Category)),
			Content: o.Content, Durable: o.Durable, SupersededBy: o.SupersededBy,
		}
		for _, label := range o.Threads {
			if id, ok := labels[label]; ok {
				op.Threads = append(op.Threads, id)
				if d := dates[label]; d.After(op.SeenAt) {
					op.SeenAt = d
				}
			}
		}
		ops = append(ops, op)
	}
	return ops
}

// userFactResponse is one fact on the read-only Memories page.
type userFactResponse struct {
	chat.UserFact
	Bucket chat.UserFactBucket `json:"bucket"`
}

// handleGetUserFacts backs the read-only Memories page: active facts in render
// order with their time bucket. There is no write endpoint — facts change only
// through the chat tools and the background refresh.
func (s *server) handleGetUserFacts(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	facts, err := s.thread.ListActiveUserFacts(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "list user facts failed")
		return
	}
	now := time.Now()
	chat.SortUserFacts(facts, now)
	out := make([]userFactResponse, len(facts))
	for i, f := range facts {
		out[i] = userFactResponse{UserFact: f, Bucket: f.Bucket(now)}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// handleGetUserDirectives backs the read-only Memories-page directives view. There
// is no create/update/delete endpoint — those go through the chat tools.
func (s *server) handleGetUserDirectives(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !requireThreadStore(w, s) {
		return
	}
	directives, err := s.thread.ListUserDirectives(r.Context(), user.ID)
	if err != nil {
		serverError(w, r, err, "list user directives failed")
		return
	}
	if directives == nil {
		directives = []chat.UserDirective{}
	}
	httpx.WriteJSON(w, http.StatusOK, directives)
}

// isFactRefreshSkippable reports refresh errors that mean "try again next
// sweep" rather than a fault worth a warning.
func isFactRefreshSkippable(err error) bool {
	return errors.Is(err, llm.ErrMemoryTruncated) || errors.Is(err, llm.ErrFactReplyMalformed)
}
