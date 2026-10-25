package policy

import (
	"net/netip"
	"testing"
)

func TestSpecialUseDestinationsRequireGrants(t *testing.T) {
	deny := mustAccess(t, ModeRestricted, Grants{})
	// Every nonpublic class is checked with and without an exact operator
	// grant. IPv4-mapped addresses use the corresponding IPv4 grant.
	for _, address := range []string{
		"0.1.2.3", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1", "172.16.0.1",
		"192.0.0.1", "192.0.0.9", "192.0.2.1", "192.31.196.1", "192.52.193.1", "192.88.99.1", "192.168.0.1", "192.175.48.1",
		"198.18.0.1", "198.19.255.254", "198.51.100.1", "203.0.113.1", "240.0.0.1", "254.255.255.254",
		"::ffff:198.18.0.1", "::ffff:192.0.2.1", "::ffff:10.0.0.1",
		"::1", "::192.0.2.1", "64:ff9b::808:808", "64:ff9b:1::1", "100::1", "100:0:0:1::1",
		"2001::1", "2001:1::1", "2001:2::1", "2001:3::1", "2001:4:112::1", "2001:10::1", "2001:20::1", "2001:30::1",
		"2001:db8::1", "2002:808:808::1", "2620:4f:8000::1", "2000::1", "2001:1000::1", "2001:4e00::1", "2003:4000::1", "2d00::1", "3ffe::1", "3fff::1", "3fff:fff::1", "4000::1", "5f00::1", "fc00::1", "fe80::1", "fec0::1",
	} {
		t.Run(address, func(t *testing.T) {
			addr := netip.MustParseAddr(address)
			if deny.AllowsAddress(addr) {
				t.Errorf("ungranted special-use address accepted: %s", addr)
			}
			unmapped := addr.Unmap()
			grant := mustAccess(t, ModeRestricted, Grants{PrivateNetworks: []netip.Prefix{netip.PrefixFrom(unmapped, unmapped.BitLen())}})
			if !grant.AllowsAddress(addr) {
				t.Errorf("explicit special-use grant ignored: %s", addr)
			}
			if !Trusted().AllowsAddress(addr) {
				t.Errorf("trusted compatibility changed: %s", addr)
			}
		})
	}
}

func TestOrdinaryPublicDestinations(t *testing.T) {
	a := mustAccess(t, ModeRestricted, Grants{})
	for _, address := range []string{"1.1.1.1", "8.8.8.8", "93.184.215.14", "192.0.1.1", "198.17.255.254", "198.20.0.1", "::ffff:8.8.8.8", "2001:4860:4860::8888", "2606:4700:4700::1111", "2001:200::1", "2001:4200::1", "2400::1", "2410::1", "2630::1", "2800::1", "2a00::1", "2a10::1", "2c00::1"} {
		if !a.AllowsAddress(netip.MustParseAddr(address)) {
			t.Errorf("public address denied: %s", address)
		}
	}
}

func TestNondestinationsCannotBeGranted(t *testing.T) {
	a := mustAccess(t, ModeRestricted, Grants{PrivateNetworks: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}})
	for _, address := range []string{"0.0.0.0", "::", "224.0.0.1", "239.255.255.255", "ff02::1", "255.255.255.255", "::ffff:0.0.0.0", "::ffff:224.0.0.1", "::ffff:255.255.255.255", "fe80::1%eth0"} {
		addr := netip.MustParseAddr(address)
		if a.AllowsAddress(addr) || Trusted().AllowsAddress(addr) {
			t.Errorf("nondestination authorized: %s", addr)
		}
	}
	if a.AllowsAddress(netip.Addr{}) {
		t.Fatal("invalid address authorized")
	}
}
