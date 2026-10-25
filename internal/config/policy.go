package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"

	"github.com/lewta/sendit/internal/policy"
	"go.yaml.in/yaml/v3"
)

func parseCapabilityRequests(data []byte) (policy.Requests, error) {
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return policy.Requests{}, errors.New("config policy: malformed YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return policy.Requests{}, errors.New("config policy: expected one YAML document")
	}
	if len(doc.Content) == 0 {
		return policy.Requests{}, nil
	}
	root := doc.Content[0]
	// Traverse aliases with an active-node set and a budget. Never let cyclic or
	// exponentially expanded YAML reach downstream decoders or validators.
	budget := 100000
	if err := inspectPolicyYAML(root, "", make(map[*yaml.Node]bool), &budget); err != nil {
		return policy.Requests{}, err
	}
	if root.Kind != yaml.MappingNode {
		return policy.Requests{}, nil
	}
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "capability_requests" {
			return decodeRequests(root.Content[i+1])
		}
	}
	return policy.Requests{}, nil
}

func inspectPolicyYAML(n *yaml.Node, path string, active map[*yaml.Node]bool, budget *int) error {
	*budget--
	if n == nil || *budget < 0 || active[n] {
		return errors.New("config policy: cyclic or excessive YAML structure")
	}
	active[n] = true
	defer delete(active, n)
	if n.Kind == yaml.AliasNode {
		return inspectPolicyYAML(n.Alias, path, active, budget)
	}
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]bool)
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if err := inspectPolicyYAML(key, path, active, budget); err != nil {
				return err
			}
			for key.Kind == yaml.AliasNode {
				key = key.Alias
			}
			if key.Kind != yaml.ScalarNode {
				return errors.New("config policy: mapping keys must be scalar")
			}
			name := strings.ToLower(strings.ReplaceAll(key.Value, "-", "_"))
			if seen[name] {
				return errors.New("config policy: duplicate or conflicting YAML keys")
			}
			seen[name] = true
			full := name
			if path != "" {
				full = path + "." + name
			}
			parts := strings.Split(name, ".")
			// User data maps are not policy namespaces. Still traverse their
			// values to detect cycles and any later merge into structural keys.
			if path == "vars" || path == "vars_file" || path == "headers" || strings.HasSuffix(path, ".vars") || strings.HasSuffix(path, ".vars_file") || strings.HasSuffix(path, ".headers") {
				parts = nil
			}
			for _, part := range parts {
				switch part {
				case "capability_requests":
					if path != "" || key.Value != "capability_requests" || n.Content[i].Kind == yaml.AliasNode {
						return errors.New("config policy: capability_requests must be a literal root key")
					}
				case "config_trust", "trust", "trusted", "restricted", "policy", "access", "grants", "allowlist", "allow_env", "allow_read_root", "allow_read_roots", "allow_output_root", "allow_overwrite", "allow_private_network", "allow_private_networks":
					return errors.New("config policy: authority fields are operator-only")
				case "env_names", "read_roots", "output_root", "overwrite", "private_networks":
					if path != "capability_requests" {
						return errors.New("config policy: capabilities must use capability_requests")
					}
				case "mode":
					value := strings.ToLower(n.Content[i+1].Value)
					if full != "pacing.mode" && (path == "" || path == "capability_requests" || value == "trusted" || value == "restricted") {
						return errors.New("config policy: trust mode is operator-only")
					}
				}
			}
			if err := inspectPolicyYAML(n.Content[i+1], full, active, budget); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range n.Content {
		if err := inspectPolicyYAML(child, path, active, budget); err != nil {
			return err
		}
	}
	return nil
}

func decodeRequests(n *yaml.Node) (policy.Requests, error) {
	var r policy.Requests
	if err := literalRequest(n); err != nil {
		return r, err
	}
	if n.Kind != yaml.MappingNode {
		return r, errors.New("config policy: capability_requests must be a mapping")
	}
	for i := 0; i < len(n.Content); i += 2 {
		name, value := n.Content[i].Value, n.Content[i+1]
		switch name {
		case "env_names", "read_roots", "private_networks":
			values, err := requestStrings(name, value)
			if err != nil {
				return r, err
			}
			switch name {
			case "env_names":
				r.EnvNames = values
			case "read_roots":
				r.ReadRoots = values
			case "private_networks":
				r.PrivateNetworks = make([]netip.Prefix, 0, len(values))
				for _, value := range values {
					prefix, err := netip.ParsePrefix(value)
					if err != nil {
						return r, errors.New("config policy: invalid private_networks CIDR")
					}
					r.PrivateNetworks = append(r.PrivateNetworks, prefix)
				}
			}
		case "output_root":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return r, errors.New("config policy: output_root must be a string")
			}
			root := value.Value
			r.OutputRoot = &root
		case "overwrite":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
				return r, errors.New("config policy: overwrite must be a boolean")
			}
			var overwrite bool
			if err := value.Decode(&overwrite); err != nil {
				return r, errors.New("config policy: invalid overwrite")
			}
			r.Overwrite = &overwrite
		default:
			return r, errors.New("config policy: unknown capability request field")
		}
	}
	return r, nil
}

func literalRequest(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" || n.Tag == "!!merge" {
		return errors.New("config policy: capability requests cannot use aliases, anchors, or merges")
	}
	for _, child := range n.Content {
		if err := literalRequest(child); err != nil {
			return err
		}
	}
	return nil
}

func requestStrings(name string, n *yaml.Node) ([]string, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("config policy: %s must be a list of strings", name)
	}
	values := make([]string, 0, len(n.Content))
	for _, child := range n.Content {
		if child.Kind != yaml.ScalarNode || child.Tag != "!!str" {
			return nil, fmt.Errorf("config policy: %s must contain strings", name)
		}
		values = append(values, child.Value)
	}
	return values, nil
}

func canonicalRequests(r policy.Requests, a policy.Access) policy.Requests {
	s := a.Summary()
	var result policy.Requests
	if r.EnvNames != nil {
		result.EnvNames = append([]string{}, s.EnvNames...)
	}
	if r.ReadRoots != nil {
		result.ReadRoots = append([]string{}, s.ReadRoots...)
	}
	if r.OutputRoot != nil {
		result.OutputRoot = &s.OutputRoot
	}
	if r.Overwrite != nil {
		result.Overwrite = &s.Overwrite
	}
	if r.PrivateNetworks != nil {
		result.PrivateNetworks = append([]netip.Prefix{}, s.PrivateNetworks...)
	}
	return result
}
