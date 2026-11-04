package loomuser

import (
	"net/http/httptest"
	"testing"
)

func TestFromRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if FromRequest(r) != "" {
		t.Error("no header must mean no user")
	}
	r.Header.Set(Header, "  u-42 ")
	if got := FromRequest(r); got != "u-42" {
		t.Errorf("got %q", got)
	}
}
