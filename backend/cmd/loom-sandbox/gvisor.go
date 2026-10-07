package main

import "strings"

// gVisor reports a fictional kernel: older releases "Linux version 4.4.0 #1 SMP
// Sun Jan 10 15:06:54 PST 2016", current ones "Linux version 4.19.0-gvisor"
// with the same build stamp. This is an operator guard against starting the
// sandbox on plain runc by mistake, not a security boundary: it runs before
// any job exists.
func isGVisorVersion(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, "-gvisor ") ||
		strings.HasPrefix(v, "Linux version 4.4.0 #1 SMP Sun Jan 10 15:06:54 PST 2016")
}
