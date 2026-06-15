package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())
	png := onePixelPNG()

	text, err := c.DescribeImage(context.Background(), png, "image/png")
	if err != nil {
		t.Fatalf("DescribeImage error = %v", err)
	}
	if !strings.Contains(text, "red bicycle") {
		t.Errorf("description = %q, want it to contain the model output", text)
	}
	if gotModel != visionModel {
		t.Errorf("request model = %q, want %q", gotModel, visionModel)
	}
	if !gotHasImage {
		t.Error("request body did not carry an image_url content part")
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
