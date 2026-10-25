package policy

import "net/netip"

// publicDestination is a deliberately conservative, auditable policy snapshot,
// not a claim that an address is reachable. All IANA special-purpose entries
// require explicit grants, including globally reachable protocol anycasts.
// IPv6 must also belong to an allocated RIR prefix: future allocations fail
// closed until this snapshot is updated or the operator grants a CIDR.
// Reviewed 2026-10-24 against:
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
// https://www.iana.org/assignments/ipv6-unicast-address-assignments/
// Prefix literals live locally; there is no mutable global policy table.
func publicDestination(addr netip.Addr) bool {
	if addr.Is4() {
		return !inPrefixes(addr,
			"0.0.0.0/8", // This network.
			"10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
			"192.0.0.0/24", // Protocol assignments, including anycast exceptions.
			"192.0.2.0/24", "192.31.196.0/24", "192.52.193.0/24", "192.88.99.0/24",
			"192.168.0.0/16", "192.175.48.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
			"224.0.0.0/4", "240.0.0.0/4", // Multicast and reserved space.
		)
	}
	if !addr.Is6() || inPrefixes(addr,
		"2001::/23", // Protocol assignments, Teredo, benchmark, ORCHID, etc.
		"2001:db8::/32", "2002::/16", "2620:4f:8000::/48", "3fff::/20",
	) {
		return false
	}
	// Only ordinary RIR allocations, excluding all IANA protocol/transition
	// allocations. This also denies unallocated 2000::/3 gaps and old 6bone.
	return inPrefixes(addr,
		"2001:200::/23", "2001:400::/23", "2001:600::/23", "2001:800::/22", "2001:c00::/23", "2001:e00::/23",
		"2001:1200::/23", "2001:1400::/22", "2001:1800::/23", "2001:1a00::/23", "2001:1c00::/22", "2001:2000::/19",
		"2001:4000::/23", "2001:4200::/23", "2001:4400::/23", "2001:4600::/23", "2001:4800::/23", "2001:4a00::/23", "2001:4c00::/23",
		"2001:5000::/20", "2001:8000::/19", "2001:a000::/20", "2001:b000::/20", "2003::/18",
		"2400::/12", "2410::/12", "2600::/12", "2610::/23", "2620::/23", "2630::/12", "2800::/12", "2a00::/12", "2a10::/12", "2c00::/12",
	)
}

func inPrefixes(addr netip.Addr, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if netip.MustParsePrefix(prefix).Contains(addr) {
			return true
		}
	}
	return false
}
