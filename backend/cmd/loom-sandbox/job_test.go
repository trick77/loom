package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidateRequestRejectsBadInput(t *testing.T) {
	big := make([]byte, maxInputBytes/2+1)
	cases := map[string]runRequest{
		"empty code":     {Code: "  \n"},
		"code too large": {Code: strings.Repeat("x", maxCodeBytes+1)},
		"path traversal": {Code: "1", Files: []wireFile{{Name: "../etc/passwd"}}},
		"slash":          {Code: "1", Files: []wireFile{{Name: "a/b.csv"}}},
		"dotfile":        {Code: "1", Files: []wireFile{{Name: ".bashrc"}}},
		"double dot":     {Code: "1", Files: []wireFile{{Name: "a..csv"}}},
		"non-ascii":      {Code: "1", Files: []wireFile{{Name: "übersicht.xlsx"}}},
		"duplicate":      {Code: "1", Files: []wireFile{{Name: "a.csv"}, {Name: "a.csv"}}},
		"too many":       {Code: "1", Files: make([]wireFile, maxInputFiles+1)},
		"too big":        {Code: "1", Files: []wireFile{{Name: "a.csv", Data: big}, {Name: "b.csv", Data: big}}},
	}
	for name, req := range cases {
		if _, err := validateRequest(req); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestValidateRequestAcceptsAliases(t *testing.T) {
	j, err := validateRequest(runRequest{
		Code:  "print(1)",
		Files: []wireFile{{Name: "input1_umsatz_2025.xlsx", Data: []byte("x")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(j.inputs) != 1 || j.timeout != maxTimeout {
		t.Fatalf("got %+v", j)
	}
}

func TestClampTimeout(t *testing.T) {
	cases := map[int64]time.Duration{
		0:         maxTimeout,
		-5:        maxTimeout,
		10:        minTimeout,
		5000:      5 * time.Second,
		3_600_000: maxTimeout,
	}
	for ms, want := range cases {
		if got := clampTimeout(ms); got != want {
			t.Errorf("clampTimeout(%d) = %v, want %v", ms, got, want)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	env := map[string]string{"SANDBOX_TOKEN": "0123456789abcdef"}
	getenv := func(k string) string { return env[k] }
	cfg, err := loadConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.slots != 2 || cfg.memLimit != 1280<<20 || cfg.diskLimit != 320<<20 || cfg.addr != defaultAddr || cfg.insecureDev {
		t.Fatalf("defaults: %+v", cfg)
	}

	env["SANDBOX_SLOTS"] = "0"
	if _, err := loadConfig(getenv); err == nil {
		t.Error("zero slots accepted")
	}
	env["SANDBOX_SLOTS"] = "1"
	env["SANDBOX_MEM_LIMIT_MB"] = "abc"
	if _, err := loadConfig(getenv); err == nil {
		t.Error("bad memory limit accepted")
	}
	env["SANDBOX_MEM_LIMIT_MB"] = "2560"
	env["SANDBOX_INSECURE_DEV"] = "1"
	cfg, err = loadConfig(getenv)
	if err != nil || cfg.memLimit != 2560<<20 || cfg.slots != 1 || !cfg.insecureDev {
		t.Fatalf("overrides: %+v %v", cfg, err)
	}

	env["SANDBOX_TOKEN"] = "short"
	if _, err := loadConfig(getenv); err == nil {
		t.Error("short token accepted")
	}
}
