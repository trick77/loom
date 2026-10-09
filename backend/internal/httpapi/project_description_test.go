package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// refreshProjectDescriptionIfDue regenerates the big-picture description from the
// project's thread titles when the titled-thread count has changed since the last
// generation and the debounce has elapsed. These tests pin that gate.

func TestRefreshProjectDescriptionIfDue_GeneratesFromTitlesOnCountChange(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		Project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Japan Trip", Description: ""},
		ProjectThreadTitles: []string{"Kyoto stays", "Osaka food"},
	}
	descCalls := 0
	s := &server{thread: store, llm: fakeChatClient{
		ProjectDescription:      "Planning a Japan trip across Kyoto and Osaka.",
		ProjectDescriptionCalls: &descCalls,
	}}

	if err := s.refreshProjectDescriptionIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectDescriptionIfDue() error: %v", err)
	}
	if descCalls != 1 {
		t.Fatalf("GenerateProjectDescription called %d times, want 1", descCalls)
	}
	if !store.ProjectDescriptionChanged || store.Project.Description != "Planning a Japan trip across Kyoto and Osaka." {
		t.Fatalf("description = %q, want generated from titles", store.Project.Description)
	}
	if store.Project.DescriptionSourceThreadCount != 2 {
		t.Fatalf("DescriptionSourceThreadCount = %d, want 2", store.Project.DescriptionSourceThreadCount)
	}
}

func TestRefreshProjectDescriptionIfDue_NoCountChangeIsNoOp(t *testing.T) {
	projectID := "proj_1"
	old := time.Now().Add(-time.Hour) // debounce window elapsed, so only the count gate matters
	store := &fakeThreadStore{
		Project: chat.Project{
			ID: projectID, UserID: testUser.ID, Name: "Japan Trip",
			Description:                  "Existing summary.",
			DescriptionSourceThreadCount: 2,
			AutoDescriptionGeneratedAt:   &old,
		},
		ProjectThreadTitles: []string{"Kyoto stays", "Osaka food"}, // count still 2
	}
	descCalls := 0
	s := &server{thread: store, llm: fakeChatClient{
		ProjectDescription:      "Must not be used.",
		ProjectDescriptionCalls: &descCalls,
	}}

	if err := s.refreshProjectDescriptionIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectDescriptionIfDue() error: %v", err)
	}
	if descCalls != 0 {
		t.Fatalf("GenerateProjectDescription called %d times, want 0 (titled-thread count unchanged)", descCalls)
	}
	if store.ProjectDescriptionChanged {
		t.Fatal("description regenerated despite unchanged titled-thread count")
	}
}

func TestRefreshProjectDescriptionIfDue_DebouncedWithinWindow(t *testing.T) {
	projectID := "proj_1"
	recent := time.Now().Add(-time.Minute) // well within memoryProjectDebounce
	store := &fakeThreadStore{
		Project: chat.Project{
			ID: projectID, UserID: testUser.ID, Name: "Japan Trip",
			Description:                  "Existing summary.",
			DescriptionSourceThreadCount: 2,
			AutoDescriptionGeneratedAt:   &recent,
		},
		ProjectThreadTitles: []string{"Kyoto stays", "Osaka food", "Tokyo museums"}, // count grew to 3
	}
	descCalls := 0
	s := &server{thread: store, llm: fakeChatClient{
		ProjectDescription:      "Must not be used yet.",
		ProjectDescriptionCalls: &descCalls,
	}}

	if err := s.refreshProjectDescriptionIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectDescriptionIfDue() error: %v", err)
	}
	if descCalls != 0 {
		t.Fatalf("GenerateProjectDescription called %d times, want 0 (debounced within %s)", descCalls, memoryProjectDebounce)
	}
	if store.ProjectDescriptionChanged {
		t.Fatal("description regenerated within the debounce window")
	}
}

func TestRefreshProjectDescriptionIfDue_RegeneratesAfterDebounceWhenCountGrew(t *testing.T) {
	projectID := "proj_1"
	old := time.Now().Add(-2 * memoryProjectDebounce) // debounce elapsed
	store := &fakeThreadStore{
		Project: chat.Project{
			ID: projectID, UserID: testUser.ID, Name: "Japan Trip",
			Description:                  "Old summary of two threads.",
			DescriptionSourceThreadCount: 2,
			AutoDescriptionGeneratedAt:   &old,
		},
		ProjectThreadTitles: []string{"Kyoto stays", "Osaka food", "Tokyo museums"}, // grew to 3
	}
	s := &server{thread: store, llm: fakeChatClient{ProjectDescription: "Japan trip spanning Kyoto, Osaka, and Tokyo."}}

	if err := s.refreshProjectDescriptionIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectDescriptionIfDue() error: %v", err)
	}
	if !store.ProjectDescriptionChanged {
		t.Fatal("description was not regenerated after debounce elapsed with a grown thread count")
	}
	if store.Project.Description != "Japan trip spanning Kyoto, Osaka, and Tokyo." {
		t.Fatalf("description = %q, want regenerated", store.Project.Description)
	}
	if store.Project.DescriptionSourceThreadCount != 3 {
		t.Fatalf("DescriptionSourceThreadCount = %d, want 3", store.Project.DescriptionSourceThreadCount)
	}
}

func TestRefreshProjectDescriptionIfDue_SkipsUserEditedDescription(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		Project: chat.Project{
			ID: projectID, UserID: testUser.ID, Name: "Japan Trip",
			Description:           "Hand-written, do not touch.",
			DescriptionUserEdited: true,
		},
		ProjectThreadTitles: []string{"Kyoto stays", "Osaka food", "Tokyo museums"},
	}
	descCalls := 0
	s := &server{thread: store, llm: fakeChatClient{
		ProjectDescription:      "Must not be used.",
		ProjectDescriptionCalls: &descCalls,
	}}

	if err := s.refreshProjectDescriptionIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectDescriptionIfDue() error: %v", err)
	}
	if descCalls != 0 {
		t.Fatalf("GenerateProjectDescription called %d times for a user-edited description, want 0", descCalls)
	}
	if store.ProjectDescriptionChanged || store.Project.Description != "Hand-written, do not touch." {
		t.Fatalf("user-edited description was modified: %q", store.Project.Description)
	}
}

func TestRefreshProjectDescriptionIfDue_NoTitledThreadsIsNoOp(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		Project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Empty"},
		ProjectThreadTitles: nil, // no meaningfully-titled threads yet
	}
	descCalls := 0
	s := &server{thread: store, llm: fakeChatClient{
		ProjectDescription:      "Must not be used.",
		ProjectDescriptionCalls: &descCalls,
	}}

	if err := s.refreshProjectDescriptionIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectDescriptionIfDue() error: %v", err)
	}
	if descCalls != 0 {
		t.Fatalf("GenerateProjectDescription called %d times with no titled threads, want 0", descCalls)
	}
	if store.ProjectDescriptionChanged {
		t.Fatal("description generated for a project with no titled threads")
	}
}
