package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

var templateName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var builtInVariables = map[string]bool{
	"uuid":      true,
	"timestamp": true,
	"seq":       true,
}

func validateRawVariableNames(data []byte) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Content) == 0 {
		return nil
	}
	root := dereferenceYAMLNode(document.Content[0])
	if root.Kind != yaml.MappingNode {
		return nil
	}

	var errs []string
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i].Value, dereferenceYAMLNode(root.Content[i+1])
		switch {
		case strings.EqualFold(key, "target_defaults"):
			errs = append(errs, rawVariableNameErrors(value, "target_defaults")...)
		case strings.EqualFold(key, "targets") && value.Kind == yaml.SequenceNode:
			for j, target := range value.Content {
				errs = append(errs, rawVariableNameErrors(dereferenceYAMLNode(target), fmt.Sprintf("targets[%d]", j))...)
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func rawVariableNameErrors(target *yaml.Node, prefix string) []string {
	if target.Kind != yaml.MappingNode {
		return nil
	}
	var errs []string
	for i := 0; i+1 < len(target.Content); i += 2 {
		field, variables := target.Content[i].Value, dereferenceYAMLNode(target.Content[i+1])
		if !strings.EqualFold(field, "vars") && !strings.EqualFold(field, "vars_file") || variables.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(variables.Content); j += 2 {
			name := variables.Content[j].Value
			if !templateName.MatchString(name) {
				errs = append(errs, fmt.Sprintf("%s.%s.%s must match [a-z][a-z0-9_]*", prefix, strings.ToLower(field), name))
			}
		}
	}
	return errs
}

func dereferenceYAMLNode(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode {
		return node.Alias
	}
	return node
}

// TemplateVariables returns the unique variables referenced by value.
func TemplateVariables(value string) ([]string, error) {
	var names []string
	seen := make(map[string]bool)
	for offset := 0; offset < len(value); {
		remaining := value[offset:]
		open := strings.Index(remaining, "{{")
		if open < 0 {
			return names, nil
		}
		start := offset + open + 2
		endOffset := strings.Index(value[start:], "}}")
		if endOffset < 0 {
			return nil, fmt.Errorf("unclosed template delimiter")
		}
		end := start + endOffset
		name := value[start:end]
		if !templateName.MatchString(name) {
			return nil, fmt.Errorf("invalid template variable %q", name)
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
		offset = end + 2
	}
	return names, nil
}

// ExpandTemplate substitutes exact template tokens without recursively
// interpreting replacement values.
func ExpandTemplate(value string, values map[string]string) string {
	if len(values) == 0 {
		return value
	}
	keys := make([]string, 0, len(values))
	for name := range values {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	pairs := make([]string, 0, len(keys)*2)
	for _, name := range keys {
		pairs = append(pairs, "{{"+name+"}}", values[name])
	}
	return strings.NewReplacer(pairs...).Replace(value)
}
