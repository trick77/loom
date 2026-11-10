package llm

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDescribeImage_usesVisionModelAndReturnsText(t *testing.T) {
	var gotModel string
	var gotHasImage bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		gotHasImage = strings.Contains(string(body), "image_url")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"A red bicycle leaning on a brick wall."},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	png := onePixelPNG()

	text, err := c.DescribeImage(context.Background(), png, "image/png")
	if err != nil {
		t.Fatalf("DescribeImage error = %v", err)
	}
	if !strings.Contains(text, "red bicycle") {
		t.Errorf("description = %q, want it to contain the model output", text)
	}
	if gotModel != testModel {
		t.Errorf("request model = %q, want %q", gotModel, testModel)
	}
	if !gotHasImage {
		t.Error("request body did not carry an image_url content part")
	}
}

func TestDescribeImage_emptyContentReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}]}`))
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	_, err := c.DescribeImage(context.Background(), onePixelPNG(), "image/png")
	if err == nil {
		t.Fatal("DescribeImage with empty content error = nil, want error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q, want it to mention empty content", err.Error())
	}
}

// An image too large to downscale is never sent to the vision model at full
// size.
func TestDescribeImage_refusesTooManyPixels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("vision model called for an oversized image")
	}))
	defer srv.Close()

	// onePixelPNG with its IHDR rewritten to declare 20000×20000.
	data := onePixelPNG()
	binary.BigEndian.PutUint32(data[16:20], 20000)
	binary.BigEndian.PutUint32(data[20:24], 20000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))

	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	if _, err := c.DescribeImage(context.Background(), data, "image/png"); err == nil || !strings.Contains(err.Error(), "too many pixels") {
		t.Fatalf("DescribeImage(oversized) error = %v, want the pixel refusal", err)
	}
}

// AskImage sends the image and the question together to the vision model and
// hands back its trimmed answer.
func TestAskImage_sendsQuestionWithImageToVisionModel(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  The y axis runs from 0 to 250.\n"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	answer, err := c.AskImage(context.Background(), onePixelPNG(), "image/png", "What range does the y axis cover?")
	if err != nil {
		t.Fatalf("AskImage error = %v", err)
	}
	if answer != "The y axis runs from 0 to 250." {
		t.Errorf("answer = %q, want the trimmed model output", answer)
	}
	if !strings.Contains(body, "image_url") {
		t.Error("request body did not carry an image_url content part")
	}
	if !strings.Contains(body, "What range does the y axis cover?") {
		t.Error("request body did not carry the question")
	}
	if !strings.Contains(body, `"model":"`+testModel+`"`) {
		t.Errorf("request did not go to the vision model %q: %s", testModel, body)
	}
}

func TestAskImage_emptyAnswerReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":" "},"finish_reason":"length"}]}`))
	}))
	defer srv.Close()

	c := mustClient(t, Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	if _, err := c.AskImage(context.Background(), onePixelPNG(), "image/png", "q"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("AskImage with empty answer error = %v, want an empty-answer error", err)
	}
}

func onePixelPNG() []byte {
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		panic(err)
	}
	return data
}
