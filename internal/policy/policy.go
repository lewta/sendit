// Package policy defines operator-owned configuration authority. Checks are
// preflight decisions, not atomic file opens or per-connection network guards.
package policy

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Mode is selected by the operator, never by configuration data.
type Mode string

const (
	ModeTrusted    Mode = "trusted"
	ModeRestricted Mode = "restricted"
)

// Grants supplies an operator ceiling. Nil fields mean the mode's default:
// unrestricted in trusted mode, denied in restricted mode. Non-nil empty lists
// explicitly deny a capability. OutputRoot pointing to "" disables output.
type Grants struct {
	EnvNames        []string       `json:"env_names"`
	ReadRoots       []string       `json:"read_roots"`
	OutputRoot      *string        `json:"output_root"`
	Overwrite       *bool          `json:"overwrite"`
	PrivateNetworks []netip.Prefix `json:"private_networks"`
}

// Requests can only narrow existing authority. Omitted (nil) fields inherit the
// current ceiling; empty lists remove it. Requests never select a trust mode.
type Requests Grants

// Access is immutable. Its zero value denies everything. Neither constructor
// inputs nor returned summaries share mutable storage with its authority.
type Access struct {
	mode                                     Mode
	envNames, readRoots                      []string
	outputRoot                               string
	overwrite                                bool
	privateNetworks                          []netip.Prefix
	allEnv, allRead, allOutput, allAddresses bool
}

// Summary describes authority without environment values or file contents.
// All* fields denote unrestricted trusted-mode capabilities.
type Summary struct {
	Mode            Mode           `json:"mode"`
	EnvNames        []string       `json:"env_names"`
	ReadRoots       []string       `json:"read_roots"`
	OutputRoot      string         `json:"output_root"`
	Overwrite       bool           `json:"overwrite"`
	PrivateNetworks []netip.Prefix `json:"private_networks"`
	AllEnv          bool           `json:"all_env"`
	AllRead         bool           `json:"all_read"`
	AllOutput       bool           `json:"all_output"`
	AllAddresses    bool           `json:"all_addresses"`
}

// New validates and copies operator grants. Explicit grants narrow even trusted
// mode, so an explicit operator denial always wins.
func New(mode Mode, grants Grants) (Access, error) {
	if mode != ModeTrusted && mode != ModeRestricted {
		return Access{}, errors.New("config policy: invalid trust mode")
	}
	g, err := normalize(grants)
	if err != nil {
		return Access{}, err
	}
	trusted := mode == ModeTrusted
	a := Access{mode: mode, allEnv: trusted, allRead: trusted, allOutput: trusted, allAddresses: trusted, overwrite: trusted}
	a.apply(g)
	return a, nil
}

// Trusted returns the compatibility policy for existing operator-owned configs.
func Trusted() Access {
	return Access{mode: ModeTrusted, allEnv: true, allRead: true, allOutput: true, allAddresses: true, overwrite: true}
}

// Mode reports the operator-selected mode, including after narrowing.
func (a Access) Mode() Mode { return a.mode }

// Summary returns an independent snapshot of the current ceiling.
func (a Access) Summary() Summary {
	return Summary{Mode: a.mode, EnvNames: slices.Clone(a.envNames), ReadRoots: slices.Clone(a.readRoots), OutputRoot: a.outputRoot, Overwrite: a.overwrite, PrivateNetworks: slices.Clone(a.privateNetworks), AllEnv: a.allEnv, AllRead: a.allRead, AllOutput: a.allOutput, AllAddresses: a.allAddresses}
}

// Narrow rejects any requested widening instead of silently granting it. It
// never changes the receiver; use the returned Access only after accepting the
// entire config/reload. Calling it with omitted fields cannot restore authority.
func (a Access) Narrow(requests Requests) (Access, error) {
	if a.mode != ModeTrusted && a.mode != ModeRestricted {
		return Access{}, errors.New("config policy: uninitialized access")
	}
	g, err := normalize(Grants(requests))
	if err != nil {
		return Access{}, err
	}
	for _, name := range g.EnvNames {
		if err := a.CheckEnvName(name); err != nil {
			return Access{}, err
		}
	}
	for _, root := range g.ReadRoots {
		if !a.allRead && !withinAny(a.readRoots, root) {
			return Access{}, fmt.Errorf("config policy: read root denied: %s", root)
		}
	}
	if g.OutputRoot != nil && *g.OutputRoot != "" && !a.allOutput && (a.outputRoot == "" || !within(a.outputRoot, *g.OutputRoot)) {
		return Access{}, fmt.Errorf("config policy: output root denied: %s", *g.OutputRoot)
	}
	if g.Overwrite != nil && *g.Overwrite && !a.overwrite {
		return Access{}, errors.New("config policy: overwrite denied")
	}
	for _, prefix := range g.PrivateNetworks {
		if !a.allAddresses && !prefixWithin(a.privateNetworks, prefix) {
			return Access{}, fmt.Errorf("config policy: private network denied: %s", prefix)
		}
	}
	a.apply(g)
	return a, nil
}

func (a *Access) apply(g Grants) {
	if g.EnvNames != nil {
		a.envNames, a.allEnv = g.EnvNames, false
	}
	if g.ReadRoots != nil {
		a.readRoots, a.allRead = g.ReadRoots, false
	}
	if g.OutputRoot != nil {
		a.outputRoot, a.allOutput = *g.OutputRoot, false
	}
	if g.Overwrite != nil {
		a.overwrite = *g.Overwrite
	}
	if g.PrivateNetworks != nil {
		a.privateNetworks, a.allAddresses = g.PrivateNetworks, false
	}
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CheckEnvName authorizes an exact, case-sensitive environment variable name.
func (a Access) CheckEnvName(name string) error {
	if !envName.MatchString(name) {
		return errors.New("config policy: invalid environment variable name")
	}
	if a.allEnv || slices.Contains(a.envNames, name) {
		return nil
	}
	return fmt.Errorf("config policy: environment variable denied: %s", name)
}

// CheckReadPath resolves existing symlinks and checks canonical containment.
// Restricted callers must supply an absolute path without traversal components.
// A missing read path is denied. This check does not prevent filesystem races.
func (a Access) CheckReadPath(path string) error {
	if a.allRead {
		return nil
	}
	p, err := canonicalPath(path, false)
	if err != nil {
		return err
	}
	if withinAny(a.readRoots, p) {
		return nil
	}
	return fmt.Errorf("config policy: read path denied: %s", p)
}

// CheckOutputPath checks output containment and an independent overwrite bit.
// Only the final component may be nonexistent; its parent must already exist.
// The caller must set overwrite for an operation that could replace contents.
// Atomic creation, overwrite detection, and symlink race defenses belong to the
// file-opening boundary, which must enforce this policy again when opening.
func (a Access) CheckOutputPath(path string, overwrite bool) error {
	if overwrite && !a.overwrite {
		return errors.New("config policy: overwrite denied")
	}
	if a.allOutput {
		return nil
	}
	p, err := canonicalPath(path, true)
	if err != nil {
		return err
	}
	if a.outputRoot != "" && within(a.outputRoot, p) && p != a.outputRoot {
		return nil
	}
	return fmt.Errorf("config policy: output path denied: %s", p)
}

// AllowsAddress permits public unicast destinations in restricted mode and
// requires a matching grant for private, loopback, link-local, or shared space.
// IPv4-mapped IPv6 is checked as IPv4. Invalid, zoned, unspecified, multicast,
// and broadcast addresses are never destinations, even with a CIDR grant.
func (a Access) AllowsAddress(addr netip.Addr) bool {
	if a.mode == "" || !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if addr.IsUnspecified() || addr.IsMulticast() || addr == netip.MustParseAddr("255.255.255.255") {
		return false
	}
	if a.allAddresses {
		return true
	}
	for _, prefix := range a.privateNetworks {
		if prefix.Contains(addr) {
			return true
		}
	}
	shared := netip.MustParsePrefix("100.64.0.0/10")
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !shared.Contains(addr)
}

func normalize(g Grants) (Grants, error) {
	g.EnvNames = slices.Clone(g.EnvNames)
	for _, name := range g.EnvNames {
		if !envName.MatchString(name) {
			return Grants{}, errors.New("config policy: invalid environment variable name")
		}
	}
	slices.Sort(g.EnvNames)
	g.EnvNames = slices.Compact(g.EnvNames)
	g.ReadRoots = slices.Clone(g.ReadRoots)
	for i, root := range g.ReadRoots {
		canonical, err := canonicalRoot(root)
		if err != nil {
			return Grants{}, err
		}
		g.ReadRoots[i] = canonical
	}
	slices.Sort(g.ReadRoots)
	g.ReadRoots = slices.Compact(g.ReadRoots)
	if g.OutputRoot != nil {
		root := *g.OutputRoot
		if root != "" {
			var err error
			root, err = canonicalRoot(root)
			if err != nil {
				return Grants{}, err
			}
		}
		g.OutputRoot = &root
	}
	if g.Overwrite != nil {
		value := *g.Overwrite
		g.Overwrite = &value
	}
	g.PrivateNetworks = slices.Clone(g.PrivateNetworks)
	for i, prefix := range g.PrivateNetworks {
		if !prefix.IsValid() || prefix.Addr().Is4In6() {
			return Grants{}, errors.New("config policy: invalid or IPv4-mapped private network prefix")
		}
		g.PrivateNetworks[i] = prefix.Masked()
	}
	slices.SortFunc(g.PrivateNetworks, func(a, b netip.Prefix) int { return strings.Compare(a.String(), b.String()) })
	g.PrivateNetworks = slices.Compact(g.PrivateNetworks)
	return g, nil
}

func canonicalRoot(path string) (string, error) {
	p, err := canonicalPath(path, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("config policy: root must be an existing directory: %s", p)
	}
	return p, nil
}

func canonicalPath(path string, missingLeaf bool) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "*?[]\x00") || strings.HasPrefix(path, "//") || strings.Contains(path, `\`) {
		return "", errors.New("config policy: path must be absolute and unambiguous")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "." || part == ".." {
			return "", errors.New("config policy: path traversal components are forbidden")
		}
	}
	clean := filepath.Clean(path)
	p, err := filepath.EvalSymlinks(clean)
	if err == nil {
		return p, nil
	}
	if missingLeaf && os.IsNotExist(err) {
		// A dangling final symlink is not a nonexistent leaf.
		if _, statErr := os.Lstat(clean); os.IsNotExist(statErr) {
			parent, parentErr := filepath.EvalSymlinks(filepath.Dir(clean))
			if parentErr == nil {
				info, statErr := os.Stat(parent)
				if statErr == nil && info.IsDir() {
					return filepath.Join(parent, filepath.Base(clean)), nil
				}
			}
		}
	}
	return "", fmt.Errorf("config policy: cannot resolve path: %s", clean)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func withinAny(roots []string, path string) bool {
	for _, root := range roots {
		if within(root, path) {
			return true
		}
	}
	return false
}

func prefixWithin(roots []netip.Prefix, prefix netip.Prefix) bool {
	for _, root := range roots {
		if root.Addr().BitLen() == prefix.Addr().BitLen() && root.Bits() <= prefix.Bits() && root.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}
