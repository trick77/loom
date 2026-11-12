package ideas

import (
	"context"
	"testing"
)

func TestFollowUpsDueAndClaimOnce(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.db.Exec(`UPDATE users SET timezone = 'Europe/Zurich' WHERE id = 'u1'`); err != nil {
		t.Fatal(err)
	}
	due := f.create(t, Input{Title: "due", Reason: "r", RemindOn: DateOf("2026-01-01")})
	later := f.create(t, Input{Title: "later", Reason: "r", RemindOn: DateOf("2026-02-01")})
	done := f.create(t, Input{Title: "done", Reason: "r", RemindOn: DateOf("2025-12-30")})
	yes := true
	_, _ = f.store.Update(ctx, f.user, done.ID, Patch{Done: &yes}, today)
	proposal, _ := f.store.Create(ctx, f.user, StateProposed, Input{Reason: "r", RemindOn: DateOf("2025-12-30")}, today)

	got, err := f.store.FollowUpsDue(ctx, today.AddDate(0, 0, 1), FollowUpCursor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Idea.ID != due.ID || got[0].Timezone != "Europe/Zurich" {
		t.Fatalf("due = %+v (later %s, done %s, proposal %s must not be listed)", got, later.ID, done.ID, proposal.ID)
	}
	ok, err := f.store.ClaimFollowUp(ctx, f.user, due.ID, "2026-01-01")
	if err != nil || !ok {
		t.Fatalf("first claim = %v %v", ok, err)
	}
	if ok, _ := f.store.ClaimFollowUp(ctx, f.user, due.ID, "2026-01-01"); ok {
		t.Fatal("a follow-up was claimed twice")
	}
	if got, _ := f.store.FollowUpsDue(ctx, today.AddDate(0, 0, 1), FollowUpCursor{}, 10); len(got) != 0 {
		t.Fatalf("claimed follow-up still due: %+v", got)
	}
	// Moving the date starts it over.
	moved, _ := f.store.Update(ctx, f.user, due.ID, Patch{RemindOn: DateOf("2026-01-01")}, today)
	if moved.RemindedAt == nil {
		t.Fatal("same date must not reset the claim")
	}
	moved, _ = f.store.Update(ctx, f.user, due.ID, Patch{RemindOn: DateOf("2026-01-02")}, today)
	if moved.RemindedAt != nil {
		t.Fatal("moved date kept the old claim")
	}
	if ok, _ := f.store.ClaimFollowUp(ctx, f.user, due.ID, "2026-01-01"); ok {
		t.Fatal("a claim for the old date went through")
	}
}

func TestFollowUpsForToday(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	day := func(s string) OptionalDate { return DateOf(s) }
	shown := f.create(t, Input{Title: "shown", Reason: "r", DueOn: day("2026-01-05"), RemindOn: day("2025-12-29")})
	f.create(t, Input{Title: "not yet", Reason: "r", DueOn: day("2026-01-20"), RemindOn: day("2026-01-13")})
	f.create(t, Input{Title: "due passed", Reason: "r", DueOn: day("2025-12-31"), RemindOn: day("2025-12-24")})
	f.create(t, Input{Title: "no due, recent", Reason: "r", RemindOn: day("2025-12-20")})
	f.create(t, Input{Title: "no due, old", Reason: "r", RemindOn: day("2025-12-01")})
	hidden := f.create(t, Input{Title: "hidden", Reason: "r", DueOn: day("2026-01-03"), RemindOn: day("2025-12-27")})
	yes := true
	_, _ = f.store.Update(ctx, f.user, hidden.ID, Patch{RemindDismissed: &yes}, today)

	got, err := f.store.FollowUpsForToday(ctx, f.user, today, 3)
	if err != nil {
		t.Fatal(err)
	}
	if titles(got) != "no due, recent,shown" {
		t.Fatalf("today = %s", titles(got))
	}
	if got[1].ID != shown.ID {
		t.Fatal("order")
	}
	if other, _ := f.store.FollowUpsForToday(ctx, "u2", today, 3); len(other) != 0 {
		t.Fatal("another user's follow-ups listed")
	}
}
