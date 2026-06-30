package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStore_UserDirectivesLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// Empty to start.
	if directives, err := store.ListUserDirectives(ctx, userID); err != nil || len(directives) != 0 {
		t.Fatalf("ListUserDirectives() initial = %v, err %v; want empty", directives, err)
	}

	first, err := store.AddUserDirective(ctx, userID, "  Always answer in metric units  ")
	if err != nil {
		t.Fatalf("AddUserDirective() error: %v", err)
	}
	if first.ID == "" || first.Content != "Always answer in metric units" {
		t.Fatalf("AddUserDirective() = %+v; want trimmed content and an id", first)
	}

	second, err := store.AddUserDirective(ctx, userID, "Call me Jan")
	if err != nil {
		t.Fatalf("AddUserDirective() error: %v", err)
	}

	// Insertion order is stable.
	directives, err := store.ListUserDirectives(ctx, userID)
	if err != nil || len(directives) != 2 {
		t.Fatalf("ListUserDirectives() = %v (len %d), err %v; want 2", directives, len(directives), err)
	}
	if directives[0].ID != first.ID || directives[1].ID != second.ID {
		t.Fatalf("directives out of insertion order: %+v", directives)
	}

	// Replace.
	replaced, found, err := store.ReplaceUserDirective(ctx, userID, first.ID, "Always use metric")
	if err != nil || !found || replaced.Content != "Always use metric" {
		t.Fatalf("ReplaceUserDirective() = %+v, found %v, err %v", replaced, found, err)
	}

	// Remove.
	removed, err := store.RemoveUserDirective(ctx, userID, first.ID)
	if err != nil || !removed {
		t.Fatalf("RemoveUserDirective() = %v, err %v; want true", removed, err)
	}
	if directives, _ := store.ListUserDirectives(ctx, userID); len(directives) != 1 || directives[0].ID != second.ID {
		t.Fatalf("after remove = %+v; want only the second", directives)
	}
}

func TestStore_AddUserDirectiveRejectsBlank(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	if _, err := store.AddUserDirective(ctx, userID, "   "); err == nil {
		t.Fatalf("AddUserDirective(blank) = nil error; want rejection")
	}
}

func TestStore_AddUserDirectiveEnforcesTotalBudget(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// Fill most of the budget.
	big := strings.Repeat("x", MaxUserDirectivesTotalLength-10)
	if _, err := store.AddUserDirective(ctx, userID, big); err != nil {
		t.Fatalf("AddUserDirective(big) error: %v", err)
	}

	// The next one pushes past the total budget and must be refused with the
	// sentinel — and must NOT be written.
	_, err := store.AddUserDirective(ctx, userID, "this will not fit")
	if !errors.Is(err, ErrDirectivesBudgetExceeded) {
		t.Fatalf("AddUserDirective(over budget) err = %v; want ErrDirectivesBudgetExceeded", err)
	}
	if directives, _ := store.ListUserDirectives(ctx, userID); len(directives) != 1 {
		t.Fatalf("over-budget add was written: %d directives", len(directives))
	}
}

func TestStore_ReplaceUserDirectiveEnforcesBudgetExcludingSelf(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// A single directive occupying most of the budget can be replaced by another
	// of similar size: the old content must not be double-counted.
	original := strings.Repeat("x", MaxUserDirectivesTotalLength-5)
	added, err := store.AddUserDirective(ctx, userID, original)
	if err != nil {
		t.Fatalf("AddUserDirective() error: %v", err)
	}
	replacement := strings.Repeat("y", MaxUserDirectivesTotalLength)
	if _, found, err := store.ReplaceUserDirective(ctx, userID, added.ID, replacement); err != nil || !found {
		t.Fatalf("ReplaceUserDirective(within budget) found %v, err %v", found, err)
	}

	// But a replacement over the per-item cap is refused.
	tooLong := strings.Repeat("z", MaxUserDirectiveLength+1)
	if _, _, err := store.ReplaceUserDirective(ctx, userID, added.ID, tooLong); !errors.Is(err, ErrDirectivesBudgetExceeded) {
		t.Fatalf("ReplaceUserDirective(too long) err = %v; want ErrDirectivesBudgetExceeded", err)
	}
}

func TestStore_ReplaceUserDirectiveMissingIDBeatsBudget(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	// Fill the budget so a budget check would trip if reached.
	if _, err := store.AddUserDirective(ctx, userID, strings.Repeat("x", MaxUserDirectivesTotalLength-5)); err != nil {
		t.Fatalf("AddUserDirective() error: %v", err)
	}

	// Replacing a non-existent id must report not-found (false, nil), NOT a budget
	// error — even though the would-be new content overshoots the budget.
	_, found, err := store.ReplaceUserDirective(ctx, userID, "bogus", strings.Repeat("y", 100))
	if err != nil {
		t.Fatalf("ReplaceUserDirective(missing id) err = %v; want nil", err)
	}
	if found {
		t.Fatalf("ReplaceUserDirective(missing id) found = true; want false")
	}
}

func TestStore_RemoveUserDirectiveMissingReturnsFalse(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)

	if found, err := store.RemoveUserDirective(ctx, userID, "nope"); err != nil || found {
		t.Fatalf("RemoveUserDirective(missing) = %v, err %v; want false", found, err)
	}
}

func TestStore_UserDirectivesScopedByUser(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	bob := insertTestUser(t, db, "bob")
	store := NewStore(db)

	added, err := store.AddUserDirective(ctx, alice, "Alice only")
	if err != nil {
		t.Fatalf("AddUserDirective() error: %v", err)
	}

	if directives, _ := store.ListUserDirectives(ctx, bob); len(directives) != 0 {
		t.Fatalf("bob sees alice's directives: %+v", directives)
	}
	// Bob cannot remove alice's directive.
	if found, _ := store.RemoveUserDirective(ctx, bob, added.ID); found {
		t.Fatalf("bob removed alice's directive")
	}
}
