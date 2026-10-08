package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var templateName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var builtInVariables = map[string]bool{
	"uuid":      true,
	"timestamp": true,
	"seq":       true,
}

// TemplateVariables returns the unique variables referenced by value.
func TemplateVariables(value string) ([]string, error) {
	var names []string
	seen := make(map[string]bool)
	for offset := 0; offset < len(value); {
		remaining := value[offset:]
		open := strings.Index(remaining, "{{")
		close := strings.Index(remaining, "}}")
		if close >= 0 && (open < 0 || close < open) {
			return nil, fmt.Errorf("unexpected closing template delimiter")
		}
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
