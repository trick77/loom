package httpapi

import (
	"fmt"
	"net"
	"syscall"
)

// specialUseRanges are the IANA special-use address blocks that net.IP's own
// predicates (IsPrivate, IsLoopback, IsLinkLocal…) do not cover. The list is
// the one github.com/trick77/webfetch uses for its own SSRF guard; it is copied
// because webfetch does not export it yet. Keep the two in sync.
var specialUseRanges = mustParseCIDRs(
	"0.0.0.0/8",       // "this host on this network"
	"100.64.0.0/10",   // carrier-grade NAT (RFC 6598)
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // TEST-NET-1 (documentation)
	"192.88.99.0/24",  // 6to4 relay anycast (deprecated)
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // TEST-NET-2 (documentation)
	"203.0.113.0/24",  // TEST-NET-3 (documentation)
	"240.0.0.0/4",     // reserved / future use (incl. 255.255.255.255)
	"::/96",           // IPv4-compatible IPv6 (deprecated; e.g. ::127.0.0.1)
	"64:ff9b::/96",    // NAT64
	"64:ff9b:1::/48",  // local-use NAT64
	"100::/64",        // discard-only
	"2001:db8::/32",   // documentation
	"2002::/16",       // 6to4
	"3fff::/20",       // documentation
	"5f00::/16",       // segment routing (SRv6)
)

// isPublicIP reports whether ip is a globally routable public unicast address a
// server-side fetch may reach. It is a strict allowlist: loopback, private
// (RFC1918 + ULA), link-local (incl. the 169.254.169.254 metadata endpoint),
// unspecified, broadcast, multicast and every special-use range are rejected.
func isPublicIP(ip net.IP) bool {
	// Normalize IPv4-mapped IPv6 (::ffff:10.0.0.1) to its IPv4 form so the checks
	// below cannot be bypassed through the mapped representation.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, r := range specialUseRanges {
		if r.Contains(ip) {
			return false
		}
	}
	return true
}

// guardPublicAddr is a net.Dialer Control hook: it runs after DNS resolution
// and refuses to connect to anything but a public address, so a hostname that
// resolves to an internal service, or a redirect there, never reaches it. The
// port is not restricted: a web source on a non-standard port is still a
// public web server, and the address check is what keeps internal services out.
func guardPublicAddr(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("favicon: unresolved address %q", address)
	}
	if !isPublicIP(ip) {
		return fmt.Errorf("favicon: refusing to connect to %s", ip)
	}
	return nil
}

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("httpapi: invalid special-use CIDR " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}
