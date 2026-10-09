package config

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ValidateHTTPVersion is shared by config, direct drivers and replay preflight.
func ValidateHTTPVersion(version int) error {
	if version < 0 || version > 2 {
		return fmt.Errorf("http_version must be 0 (auto), 1 (HTTP/1.1), or 2 (HTTPS HTTP/2); HTTP/3 is not supported")
	}
	return nil
}

// walkHTTPVersionMapping includes YAML aliases/merges, before Viper can coerce
// nulls, bools or strings into an apparently valid integer policy.
func walkHTTPVersionMapping(n *yaml.Node, active map[*yaml.Node]bool, visit func(string, *yaml.Node) error) error {
	if n == nil {
		return nil
	}
	if active[n] {
		return fmt.Errorf("cyclic YAML mapping in HTTP version configuration")
	}
	active[n] = true
	defer delete(active, n)
	switch n.Kind {
	case yaml.AliasNode:
		return walkHTTPVersionMapping(n.Alias, active, visit)
	case yaml.SequenceNode:
		for _, child := range n.Content {
			if err := walkHTTPVersionMapping(child, active, visit); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := dereferenceYAMLNode(n.Content[i]), n.Content[i+1]
			if key.Tag == "!!merge" {
				if err := walkHTTPVersionMapping(value, active, visit); err != nil {
					return err
				}
			} else {
				name := strings.ToLower(key.Value)
				// Viper expands dotted mapping keys before weak decoding. Preserve
				// the original value node/tag while visiting the equivalent path.
				if first, rest, dotted := strings.Cut(name, "."); dotted {
					name = first
					value = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: rest}, value}}
				}
				if err := visit(name, value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateRawHTTPVersions(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		return nil
	}
	checkTarget := func(target *yaml.Node, prefix string) error {
		return walkHTTPVersionMapping(target, make(map[*yaml.Node]bool), func(field string, value *yaml.Node) error {
			if field != "http" {
				return nil
			}
			return walkHTTPVersionMapping(value, make(map[*yaml.Node]bool), func(field string, value *yaml.Node) error {
				if field != "http_version" {
					return nil
				}
				value = dereferenceYAMLNode(value)
				var version int
				if value.Tag != "!!int" || value.Decode(&version) != nil {
					return fmt.Errorf("%s.http.http_version must be an integer 0, 1, or 2", prefix)
				}
				if err := ValidateHTTPVersion(version); err != nil {
					return fmt.Errorf("%s.http.%w", prefix, err)
				}
				return nil
			})
		})
	}
	return walkHTTPVersionMapping(doc.Content[0], make(map[*yaml.Node]bool), func(field string, value *yaml.Node) error {
		switch field {
		case "target_defaults":
			return checkTarget(value, "target_defaults")
		case "targets":
			value = dereferenceYAMLNode(value)
			if value.Kind == yaml.MappingNode {
				return checkTarget(value, "targets[0]")
			}
			if value.Kind == yaml.SequenceNode {
				for i, target := range value.Content {
					if err := checkTarget(target, fmt.Sprintf("targets[%d]", i)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
