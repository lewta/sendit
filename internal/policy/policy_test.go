package policy

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func mustAccess(t *testing.T, mode Mode, grants Grants) Access {
	t.Helper()
	a, err := New(mode, grants)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRestrictedGrantsAndImmutableOwnership(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "input")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := Grants{EnvNames: []string{"TOKEN", "TOKEN"}, ReadRoots: []string{root}, OutputRoot: ptr(root), Overwrite: ptr(true), PrivateNetworks: []netip.Prefix{netip.MustParsePrefix("10.1.2.3/8")}}
	a := mustAccess(t, ModeRestricted, g)
	g.EnvNames[0] = "OTHER"
	g.ReadRoots[0] = "/"
	*g.OutputRoot = "/"
	*g.Overwrite = false
	g.PrivateNetworks[0] = netip.MustParsePrefix("0.0.0.0/0")
	s := a.Summary()
	if !reflect.DeepEqual(s.EnvNames, []string{"TOKEN"}) || s.PrivateNetworks[0].String() != "10.0.0.0/8" {
		t.Fatalf("not canonical: %+v", s)
	}
	s.EnvNames[0], s.ReadRoots[0] = "OTHER", "/"
	s.PrivateNetworks[0] = netip.MustParsePrefix("0.0.0.0/0")
	if a.CheckEnvName("TOKEN") != nil || a.CheckEnvName("OTHER") == nil {
		t.Fatal("caller mutated env authority")
	}
	if a.CheckReadPath(file) != nil || a.CheckReadPath(filepath.Join(t.TempDir(), "outside")) == nil {
		t.Fatal("caller mutated read authority")
	}
	if a.CheckOutputPath(filepath.Join(root, "new"), true) != nil || a.CheckOutputPath(filepath.Join(t.TempDir(), "new"), false) == nil {
		t.Fatal("caller mutated output authority")
	}
	if !a.AllowsAddress(netip.MustParseAddr("10.1.2.3")) || a.AllowsAddress(netip.MustParseAddr("192.168.1.2")) {
		t.Fatal("caller mutated address authority")
	}
	deny := mustAccess(t, ModeRestricted, Grants{})
	if deny.CheckEnvName("TOKEN") == nil || deny.CheckReadPath(file) == nil || deny.CheckOutputPath(filepath.Join(root, "new"), false) == nil {
		t.Fatal("no grants must deny")
	}
	if (Access{}).CheckEnvName("TOKEN") == nil || (Access{}).AllowsAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("zero policy must fail closed")
	}
}

func TestInvalidOperatorGrants(t *testing.T) {
	for _, mode := range []Mode{"", "TRUSTED", "invalid"} {
		if _, err := New(mode, Grants{}); err == nil {
			t.Fatalf("accepted mode %q", mode)
		}
	}
	for _, name := range []string{"", " ", "A*", "A?", "A=B", "A B", "0NAME", "A\x00B"} {
		if _, err := New(ModeRestricted, Grants{EnvNames: []string{name}}); err == nil {
			t.Fatalf("accepted name %q", name)
		}
	}
	for _, root := range []string{"", ".", "relative", "/tmp/*", "/tmp/../tmp", "/tmp/./x", `C:\data`, "//ambiguous/root", "/tmp/no-such-sendit-policy-root"} {
		if _, err := New(ModeRestricted, Grants{ReadRoots: []string{root}}); err == nil {
			t.Fatalf("accepted root %q", root)
		}
	}
	for _, prefix := range []netip.Prefix{{}, netip.MustParsePrefix("::ffff:10.0.0.0/104")} {
		if _, err := New(ModeRestricted, Grants{PrivateNetworks: []netip.Prefix{prefix}}); err == nil {
			t.Fatalf("accepted ambiguous prefix %v", prefix)
		}
	}
}

func TestPathContainment(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "inside")
	outside := root + "-sibling"
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	for _, p := range []string{inside, filepath.Join(outside, "data")} {
		if err := os.WriteFile(p, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := mustAccess(t, ModeRestricted, Grants{ReadRoots: []string{root}, OutputRoot: ptr(root)})
	for _, tc := range []struct {
		path         string
		read, output bool
	}{
		{inside, true, true},
		{filepath.Join(root, "missing"), false, true},
		{filepath.Join(root, "missing", "leaf"), false, false},
		{filepath.Join(outside, "data"), false, false},
		{root + "/../" + filepath.Base(outside) + "/data", false, false},
		{"relative", false, false},
	} {
		if got := a.CheckReadPath(tc.path) == nil; got != tc.read {
			t.Errorf("read %q: %v", tc.path, got)
		}
		if got := a.CheckOutputPath(tc.path, false) == nil; got != tc.output {
			t.Errorf("output %q: %v", tc.path, got)
		}
	}
	if a.CheckOutputPath(inside, true) == nil {
		t.Fatal("overwrite needs a separate grant")
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges on Windows")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inside, filepath.Join(root, "safe")); err != nil {
		t.Fatal(err)
	}
	if a.CheckReadPath(filepath.Join(root, "safe")) != nil {
		t.Fatal("in-root symlink should resolve")
	}
	if a.CheckReadPath(filepath.Join(root, "escape", "data")) == nil || a.CheckOutputPath(filepath.Join(root, "escape", "new"), false) == nil {
		t.Fatal("symlink escaped root")
	}
}

func TestAddressClasses(t *testing.T) {
	a := mustAccess(t, ModeRestricted, Grants{})
	for _, tc := range []struct {
		addr  string
		allow bool
	}{
		{"8.8.8.8", true}, {"2606:4700:4700::1111", true}, {"::ffff:8.8.8.8", true},
		{"127.0.0.1", false}, {"10.1.2.3", false}, {"172.16.0.1", false}, {"192.168.1.1", false},
		{"169.254.169.254", false}, {"224.0.0.1", false}, {"0.0.0.0", false}, {"255.255.255.255", false},
		{"::1", false}, {"fc00::1", false}, {"fe80::1", false}, {"ff02::1", false}, {"::", false},
		{"::ffff:127.0.0.1", false}, {"100.64.0.1", false}, {"fe80::1%en0", false},
	} {
		if got := a.AllowsAddress(netip.MustParseAddr(tc.addr)); got != tc.allow {
			t.Errorf("%s: %v", tc.addr, got)
		}
	}
	b := mustAccess(t, ModeRestricted, Grants{PrivateNetworks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::/0")}})
	if !b.AllowsAddress(netip.MustParseAddr("::ffff:127.0.0.1")) || b.AllowsAddress(netip.MustParseAddr("10.0.0.1")) {
		t.Fatal("prefix containment changed")
	}
	if b.AllowsAddress(netip.MustParseAddr("::")) || b.AllowsAddress(netip.MustParseAddr("ff02::1")) || b.AllowsAddress(netip.Addr{}) {
		t.Fatal("nondestination address authorized")
	}
}

func TestNarrowingIsMonotonic(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	a := mustAccess(t, ModeRestricted, Grants{EnvNames: []string{"A", "B"}, ReadRoots: []string{root}, OutputRoot: ptr(root), Overwrite: ptr(true), PrivateNetworks: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	b, err := a.Narrow(Requests{EnvNames: []string{"A"}, ReadRoots: []string{sub}, OutputRoot: ptr(sub), Overwrite: ptr(false), PrivateNetworks: []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []Requests{
		{EnvNames: []string{"B"}}, {ReadRoots: []string{root}}, {OutputRoot: ptr(root)}, {Overwrite: ptr(true)}, {PrivateNetworks: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
	} {
		if _, err := b.Narrow(request); err == nil {
			t.Fatalf("accepted re-expansion: %+v", request)
		}
	}
	c, err := b.Narrow(Requests{EnvNames: []string{}, ReadRoots: []string{}, OutputRoot: ptr(""), PrivateNetworks: []netip.Prefix{}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.Narrow(Requests{})
	if err != nil || d.CheckEnvName("A") == nil || d.AllowsAddress(netip.MustParseAddr("10.1.2.3")) {
		t.Fatal("omission restored authority")
	}
	if a.CheckEnvName("B") != nil {
		t.Fatal("narrow mutated prior access")
	}
	trusted := mustAccess(t, ModeTrusted, Grants{})
	if trusted.CheckEnvName("ANY_NAME") != nil || !trusted.AllowsAddress(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("trusted compatibility")
	}
	narrowTrusted, err := trusted.Narrow(Requests{EnvNames: []string{}})
	if err != nil || narrowTrusted.CheckEnvName("ANY_NAME") == nil {
		t.Fatal("trusted narrowing failed")
	}
	if _, err := narrowTrusted.Narrow(Requests{EnvNames: []string{"ANY_NAME"}}); err == nil {
		t.Fatal("trusted mode restored removed capability")
	}
}
