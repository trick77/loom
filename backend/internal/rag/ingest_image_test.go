package rag

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type stubDescriber struct {
	gotMIME string
	gotData []byte
	text    string
	err     error
}

func (s *stubDescriber) DescribeImage(_ context.Context, data []byte, mime string) (string, error) {
	s.gotMIME = mime
	s.gotData = data
	return s.text, s.err
}

func TestExtractContent_routesImagesToDescriber(t *testing.T) {
	desc := &stubDescriber{text: "A bar chart of quarterly revenue."}
	ing := &Ingester{describer: desc}

	got, err := ing.extractContent(context.Background(), "chart.png", "image/png", strings.NewReader("PNGDATA"))
	if err != nil {
		t.Fatalf("extractContent error = %v", err)
	}
	if got != desc.text {
		t.Errorf("text = %q, want %q", got, desc.text)
	}
	if desc.gotMIME != "image/png" {
		t.Errorf("describer mime = %q, want image/png", desc.gotMIME)
	}
	if string(desc.gotData) != "PNGDATA" {
		t.Errorf("describer data = %q, want PNGDATA", desc.gotData)
	}
}

func TestExtractContent_describerErrorPropagates(t *testing.T) {
	desc := &stubDescriber{err: errors.New("vision boom")}
	ing := &Ingester{describer: desc}
	if _, err := ing.extractContent(context.Background(), "x.png", "image/png", strings.NewReader("x")); err == nil {
		t.Fatal("extractContent with describer error = nil, want error")
	}
}

func TestExtractContent_imageWithoutDescriberErrors(t *testing.T) {
	ing := &Ingester{} // describer nil
	if _, err := ing.extractContent(context.Background(), "x.png", "image/png", strings.NewReader("x")); err == nil {
		t.Fatal("extractContent(image, nil describer) error = nil, want error")
	}
}

type stubExtractor struct{ text string }

func (s stubExtractor) Extract(_ context.Context, _, _ string, _ io.Reader) (string, error) {
	return s.text, nil
}

func TestExtractContent_nonImageUsesExtractor(t *testing.T) {
	ing := &Ingester{extractor: stubExtractor{text: "plain text body"}}
	got, err := ing.extractContent(context.Background(), "notes.txt", "text/plain; charset=utf-8", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("extractContent error = %v", err)
	}
	if got != "plain text body" {
		t.Errorf("text = %q, want extractor output", got)
	}
}
