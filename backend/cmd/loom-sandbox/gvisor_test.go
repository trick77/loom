package main

import "testing"

func TestIsGVisorVersion(t *testing.T) {
	cases := map[string]bool{
		"Linux version 4.19.0-gvisor #1 SMP Sun Jan 10 15:06:54 PST 2016\n":                         true,
		"Linux version 4.4.0 #1 SMP Sun Jan 10 15:06:54 PST 2016":                                   true,
		"Linux version 6.8.0-45-generic (buildd@lcy02-amd64-115) (gcc 13.2.0) #45-Ubuntu SMP":       false,
		"Linux version 6.10.14-linuxkit (root@buildkitsandbox) #1 SMP Sat May 17 08:28:57 UTC 2025": false,
		"": false,
	}
	for v, want := range cases {
		if got := isGVisorVersion(v); got != want {
			t.Errorf("isGVisorVersion(%q) = %v, want %v", v, got, want)
		}
	}
}
