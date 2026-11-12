package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/ideas"
	"github.com/trick77/loom/internal/schedule"
)

const (
	// ideaFollowUpEvery is how often the schedule worker looks for follow-ups
	// whose date has come.
	ideaFollowUpEvery = 10 * time.Minute
	// ideaFollowUpHour and ideaFollowUpLastHour bound the local hours a
	// follow-up goes out: never in the night. One set late in the evening
	// for today (due tomorrow) waits for the next morning.
	ideaFollowUpHour     = 8
	ideaFollowUpLastHour = 21
	// ideaFollowUpBatch bounds one look; the rest wait for the next.
	ideaFollowUpBatch = 100
)

// sendIdeaFollowUps notifies (and pushes) every idea whose follow-up date
// has come in its owner's zone, from 8 in the morning, once. Only stored
// fields go out (title, reason); no model is asked. It runs with schedules:
// notifications live in the schedule store. Without them the start page
// still lists the follow-ups.
func (s *server) sendIdeaFollowUps(ctx context.Context) {
	if s.ideas == nil || s.schedules == nil {
		return
	}
	now := s.clock()
	// The latest date anywhere (UTC+14), as a UTC date whatever the server's
	// zone: a candidate is checked against its owner's.
	latest := now.UTC().Add(14 * time.Hour)
	var cursor ideas.FollowUpCursor
	for ctx.Err() == nil {
		page, err := s.ideas.FollowUpsDue(ctx, latest, cursor, ideaFollowUpBatch)
		if err != nil {
			slog.Warn("list idea follow-ups failed", "err", err)
			return
		}
		for _, f := range page {
			s.sendIdeaFollowUp(ctx, f, now)
		}
		if len(page) < ideaFollowUpBatch {
			return
		}
		last := page[len(page)-1].Idea
		cursor = ideas.FollowUpCursor{RemindOn: *last.RemindOn, ID: last.ID}
	}
}

// sendIdeaFollowUp notifies one candidate once its day and hour have come in
// its owner's zone. One that went stale meanwhile (its due date passed, or
// two weeks without one: loom was down, or the idea predates the worker) is
// claimed without a word: a "follow up before the date" after the date is
// noise.
func (s *server) sendIdeaFollowUp(ctx context.Context, f ideas.FollowUp, now time.Time) {
	idea := f.Idea
	// The user's zone as everywhere else (the start page's today too).
	local := now.In(userZone(auth.User{Timezone: f.Timezone}))
	today := ideas.FormatDate(local)
	if idea.RemindOn == nil || *idea.RemindOn > today ||
		local.Hour() < ideaFollowUpHour || local.Hour() >= ideaFollowUpLastHour {
		return
	}
	stale := (idea.DueOn != nil && *idea.DueOn < today) ||
		(idea.DueOn == nil && *idea.RemindOn < ideas.FormatDate(local.AddDate(0, 0, -ideas.FollowUpWindowDays)))
	claimed, err := s.ideas.ClaimFollowUp(ctx, idea.UserID, idea.ID, *idea.RemindOn)
	if err != nil {
		slog.Warn("claim idea follow-up failed", "idea_id", idea.ID, "err", err)
		return
	}
	if !claimed || stale {
		return
	}
	id := idea.ID
	s.notify(ctx, idea.UserID, schedule.Notification{
		Kind:   schedule.KindIdeaDue,
		IdeaID: &id,
		Title:  idea.Title,
		Body:   idea.Reason,
	})
}
