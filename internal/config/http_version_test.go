package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestHTTPVersionValues(t *testing.T) {
	for _, value := range []string{"0", "1", "2"} {
		t.Run(value, func(t *testing.T) {
			cfg, err := Load(writeTemp(t, "targets:\n  - url: https://example.com\n    type: http\n    weight: 1\n    http: {http_version: "+value+"}\n"))
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(cfg.Targets[0].HTTP.HTTPVersion) != value {
				t.Fatal("policy not decoded")
			}
		})
	}
	for _, value := range []string{"-1", "3", "4", "true", "false", `"2"`, "null", "2.0", "2.5", ".inf", ".nan", "18446744073709551617", "[]", "{}"} {
		for _, where := range []string{"target", "defaults"} {
			t.Run(where+value, func(t *testing.T) {
				text := "targets:\n  - url: https://example.com\n    type: http\n    weight: 1\n"
				if where == "target" {
					text += "    http: {http_version: " + value + "}\n"
				} else {
					text += "target_defaults:\n  http: {http_version: " + value + "}\n"
				}
				_, err := Load(writeTemp(t, text))
				if err == nil || !strings.Contains(err.Error(), "http_version") {
					t.Fatalf("%s accepted or bad error: %v", value, err)
				}
			})
		}
	}
}

func TestHTTPVersionDefaultsAndSchemes(t *testing.T) {
	targets := writeTempFile(t, "targets.txt", "https://example.com http\n")
	cfg, err := Load(writeTemp(t, fmt.Sprintf("targets_file: %q\ntarget_defaults:\n  http: {http_version: 2}\ntargets:\n  - url: http://inline.example.com\n    type: http\n    weight: 1\n", targets)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Targets[0].HTTP.HTTPVersion != 0 || cfg.Targets[1].HTTP.HTTPVersion != 2 {
		t.Fatal("incorrect defaults inheritance")
	}
	for _, tc := range []struct {
		url   string
		valid bool
	}{{"http://example.com", false}, {"ftp://example.com", false}, {"https://{{host}}/x", true}, {"{{scheme}}://example.com", true}, {"{{endpoint}}", true}} {
		yaml := "targets:\n  - url: '" + tc.url + "'\n    type: http\n    weight: 1\n    http: {http_version: 2}\n    vars: {host: [example.com], scheme: [https], endpoint: ['https://example.com']}\n"
		_, err := Load(writeTemp(t, yaml))
		if (err == nil) != tc.valid {
			t.Fatalf("URL %s error %v", tc.url, err)
		}
	}
}

func TestHTTPVersionRawAliasesAndMerges(t *testing.T) {
	for _, shape := range []string{
		"h: &h {http_version: %s}\ntargets:\n  - url: https://example.com\n    type: http\n    weight: 1\n    http: *h\n",
		"h: &h {http_version: %s}\ntargets:\n  - url: https://example.com\n    type: http\n    weight: 1\n    http: {<<: *h}\n",
		"v: &v %s\ntargets:\n  - url: https://example.com\n    type: http\n    weight: 1\n    http: {http_version: *v}\n",
		"d: &d {http: {http_version: %s}}\ntarget_defaults: {<<: *d}\ntargets:\n  - url: https://example.com\n    type: http\n    weight: 1\n",
	} {
		for _, value := range []string{"2", "true", "null", `"2"`} {
			_, err := Load(writeTemp(t, fmt.Sprintf(shape, value)))
			if (err == nil) != (value == "2") {
				t.Fatalf("value %s in %s: %v", value, shape, err)
			}
		}
	}
	// A template variable named http_version is not an HTTP config field.
	if _, err := Load(writeTemp(t, "targets:\n  - url: https://example.com/{{http_version}}\n    type: http\n    weight: 1\n    vars: {http_version: [hello]}\n")); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPVersionProgrammaticValidation(t *testing.T) {
	cfg, err := Load(writeTemp(t, "targets:\n  - url: https://example.com\n    type: http\n    weight: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Targets[0].HTTP.HTTPVersion = 3
	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "targets[0].http.http_version") {
		t.Fatal(err)
	}
	cfg.Targets[0].HTTP.HTTPVersion = 0
	cfg.TargetDefaults.HTTP.HTTPVersion = -1
	if err := validate(cfg); err == nil || !strings.Contains(err.Error(), "target_defaults.http.http_version") {
		t.Fatal(err)
	}
}
