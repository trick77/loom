package rag

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRetrieve_logsASlowSearch(t *testing.T) {
	// A vector search reading a bloated vec_chunks took minutes in rongo and
	// was invisible until the request died. A slow one says so, with what it
	// searched.
	s, _ := newTestStore(t)
	seedEmbeddedDocument(t, s, "d1", "alpha")
	logs := captureLogs(t)
	threshold := slowVectorSearch
	slowVectorSearch = 0
	t.Cleanup(func() { slowVectorSearch = threshold })
	p1 := "p1"

	if _, err := s.Retrieve(context.Background(), "u1", &p1, nil, unit(), 5); err != nil {
		t.Fatalf("Retrieve() err = %v", err)
	}

	r, ok := logs.find("slow vector search")
	if !ok {
		t.Fatalf("no slow search line, records = %v", logs.records)
	}
	a := attrsOf(r)
	for k, v := range map[string]string{"user": "u1", "scopes": "[ p1]", "k": "5", "hits": "1"} {
		if a[k] != v {
			t.Errorf("attr %s = %q, want %q (all: %v)", k, a[k], v, a)
		}
	}
	if a["took"] == "" {
		t.Errorf("no took attr: %v", a)
	}
}

func TestRetrieve_quietWhenFast(t *testing.T) {
	s, _ := newTestStore(t)
	logs := captureLogs(t)

	if _, err := s.Retrieve(context.Background(), "u1", nil, nil, unit(), 5); err != nil {
		t.Fatalf("Retrieve() err = %v", err)
	}
	if logs.len() != 0 {
		t.Errorf("records = %v, want none", logs.records)
	}
}

func TestRetrieve_anInterruptSaysSo(t *testing.T) {
	// sqlite-vec reports an interrupt as "SQL logic error: chunks iter
	// error"; the error has to name the cancel and how long the search ran.
	s, _ := newTestStore(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("client went away"))

	_, err := s.Retrieve(ctx, "u1", nil, nil, unit(), 5)

	if err == nil || !strings.Contains(err.Error(), "vector search interrupted after") ||
		!strings.Contains(err.Error(), "client went away") {
		t.Errorf("err = %v, want the interrupt, its duration and its cause", err)
	}
}
