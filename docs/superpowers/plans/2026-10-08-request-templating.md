# Request Templating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand custom and built-in variables once per dispatched request across target URLs, HTTP/gRPC bodies, and WebSocket messages.

**Architecture:** Configuration loading resolves newline-delimited variable files and validates the complete template vocabulary. The weighted selector keeps per-target sequence state and expands a copied target immediately after selection, before the engine derives the rate-limit domain or invokes a driver.

**Tech Stack:** Go 1.26.6+, Viper/mapstructure YAML configuration, Cobra CLI, Go standard library testing and fuzzing.

**Spec:** `docs/superpowers/specs/2026-10-08-request-templating-design.md`

## Global Constraints

- Do not add a dependency; use the Go standard library for parsing, UUIDv4 generation, file loading, and synchronization.
- Preserve O(1) weighted target selection.
- Keep all drivers unchanged; they receive fully expanded `task.Task` values.
- Template expansion is one pass and must not mutate loaded configuration.
- Variable names match `[a-z][a-z0-9_]*`; `uuid`, `timestamp`, and `seq` are reserved.
- Relative `vars_file` paths resolve from the main YAML configuration directory.
- A failed startup or reload must reject invalid templates before replacing runtime state.
- Update `CHANGELOG.md`, `ROADMAP.md`, CLI help, root docs, Hugo docs, examples, and tests in the same pull request.
- Run `make verify` before opening the pull request.

## Review Focus

- Candidate values containing braces, dollar signs, backslashes, JSON, URL delimiters, or Unicode are inserted literally and never recursively expanded; Task 2 tests this.
- Duplicate inline/file names and reserved built-in names fail with deterministic target-specific diagnostics; Task 1 tests this.
- Relative files are resolved from the YAML directory even when the process working directory differs; Task 1 tests this.
- Concurrent picks preserve unique per-target sequence values and do not mutate shared WebSocket slices; Task 2 runs under the race detector.
- An expanded hostname reaches engine domain classification before rate limiting, and expanded path/body reach the HTTP server; Task 3 tests the end-to-end dispatch path.

---

### Task 1: Configuration And Template Validation

**Files:**
- Modify: `internal/config/schema.go:17-29,80-92`
- Modify: `internal/config/config.go:19-60,132-203,294-341`
- Create: `internal/config/template.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config_fuzz_test.go`

**Interfaces:**
- Consumes: existing `Config`, `TargetConfig`, `TargetDefaultsConfig`, and `Load(path string)`.
- Produces: `TargetConfig.Vars map[string][]string`, `TargetConfig.VarsFile map[string]string`, `TemplateVariables(string) ([]string, error)`, and `ExpandTemplate(string, map[string]string) string` for Task 2.

- [ ] **Step 1: Add failing YAML and file-loading tests**

Add table-driven tests to `internal/config/config_test.go` that load temporary YAML and assert:

```go
func TestLoadTemplateVariables(t *testing.T) {
	dir := t.TempDir()
	valuesPath := filepath.Join(dir, "regions.txt")
	if err := os.WriteFile(valuesPath, []byte("us-east\n\neu-west\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, dir, `
targets:
  - url: https://{{region}}.example.com/users/{{user_id}}
    type: http
    weight: 1
    vars:
      user_id: [alice, bob]
    vars_file:
      region: regions.txt
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Targets[0].Vars["region"]; !slices.Equal(got, []string{"us-east", "eu-west"}) {
		t.Fatalf("region values = %v", got)
	}
}
```

Cover absolute paths, a changed working directory, inherited `target_defaults`, unreadable/blank files, duplicate inline/file names, invalid/empty/reserved names, empty candidates, malformed delimiters, and unknown placeholders in every supported field. Use substring assertions that include `targets[N]` and the field name.

- [ ] **Step 2: Run the focused tests and confirm they fail**

Run: `go test ./internal/config -run 'TestLoadTemplate|TestValidateTemplate'`

Expected: FAIL because the schema fields and template validation do not exist.

- [ ] **Step 3: Add schema fields and shared template helpers**

Add to both `TargetDefaultsConfig` and `TargetConfig` in `internal/config/schema.go`:

```go
Vars     map[string][]string `mapstructure:"vars"`
VarsFile map[string]string   `mapstructure:"vars_file"`
```

Create `internal/config/template.go` with a package-level identifier expression and these helpers:

```go
var templateName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TemplateVariables(value string) ([]string, error)
func ExpandTemplate(value string, values map[string]string) string
```

`TemplateVariables` must scan `{{name}}` tokens, reject unmatched or malformed delimiters, and return each referenced name once in first-seen order. `ExpandTemplate` must replace exact tokens in one pass, using a `strings.Builder` or `strings.NewReplacer` so replacement text is never reparsed.

- [ ] **Step 4: Load variable files before aggregate validation**

In `Load`, call a new helper after `loadTargetsFile` and before `validate`:

```go
if err := loadVariableFiles(&cfg, filepath.Dir(path)); err != nil {
	return nil, fmt.Errorf("loading template variables: %w", err)
}
```

Implement:

```go
func loadVariableFiles(cfg *Config, configDir string) error
```

For each target, sort `VarsFile` keys, reject a duplicate key already present in `Vars`, resolve relative paths with `filepath.Join(configDir, path)`, read with `os.ReadFile`, split lines, trim whitespace, discard blank lines, reject an empty result, clone the inline map/slices, and store loaded values in `Vars`. Copy `TargetDefaults.Vars` and `TargetDefaults.VarsFile` into targets created by `loadTargetsFile` before this helper runs.

- [ ] **Step 5: Validate variables and supported template surfaces**

Extend target validation to sort custom variable names and reject invalid names, reserved names, empty lists, and empty candidate values. For URL, HTTP body, gRPC body, and each WebSocket message, call `TemplateVariables`; append malformed syntax errors and reject names absent from both the target's effective `Vars` and the built-in set:

```go
var builtInVariables = map[string]bool{
	"uuid": true, "timestamp": true, "seq": true,
}
```

Keep error aggregation deterministic and preserve existing target validation.

- [ ] **Step 6: Add fuzz seeds for template syntax**

Extend `internal/config/config_fuzz_test.go` with valid and malformed seeds containing `{{name}}`, unmatched braces, Unicode, and escaped JSON. Assert only that parsing and validation never panic.

- [ ] **Step 7: Run configuration tests**

Run: `go test ./internal/config`

Expected: PASS.

- [ ] **Step 8: Commit the configuration unit**

```bash
git add internal/config/schema.go internal/config/config.go internal/config/template.go internal/config/config_test.go internal/config/config_fuzz_test.go
git commit -m "feat: validate request template variables"
```

### Task 2: Per-Request Selector Expansion

**Files:**
- Modify: `internal/task/task.go`
- Modify: `internal/task/task_test.go`
- Modify: `internal/task/task_fuzz_test.go`
- Modify: `internal/task/selector_bench_test.go`

**Interfaces:**
- Consumes: `config.TemplateVariables`, `config.ExpandTemplate`, and effective `TargetConfig.Vars` from Task 1.
- Produces: unchanged `NewSelector([]config.TargetConfig) (*Selector, error)` and `Pick() Task`, plus `Examples() []Task` for Task 3.

- [ ] **Step 1: Add failing selector expansion tests**

Add focused tests proving one selected value is reused across every surface:

```go
func TestPickExpandsOneValueAcrossRequest(t *testing.T) {
	target := config.TargetConfig{
		URL: "https://example.com/{{user}}/{{seq}}/{{uuid}}/{{timestamp}}",
		Type: "http", Weight: 1,
		Vars: map[string][]string{"user": {"alice"}},
		HTTP: config.HTTPConfig{Body: `{"user":"{{user}}","seq":{{seq}}}`},
		GRPC: config.GRPCConfig{Body: `{"user":"{{user}}"}`},
		WebSocket: config.WebSocketConfig{SendMessages: []string{"{{user}}-{{seq}}"}},
	}
	sel, err := NewSelector([]config.TargetConfig{target})
	if err != nil {
		t.Fatal(err)
	}
	got := sel.Pick()
	// Assert alice and sequence 1 appear consistently; parse UUID and timestamp by shape/range.
}
```

Also test candidate membership, sequence `1, 2, 3`, independent counters for duplicate target entries, UUID version/variant bits, literal non-recursive replacement, no mutation of the source target or WebSocket slice, untemplated target behavior, and one example per target.

- [ ] **Step 2: Run selector tests and confirm they fail**

Run: `go test ./internal/task -run 'TestPickExpand|TestExamples|TestTemplate'`

Expected: FAIL because `Pick` returns raw target fields and `Examples` does not exist.

- [ ] **Step 3: Compile template metadata in `NewSelector`**

Extend `Selector` with per-target referenced-name metadata and atomic sequence counters while leaving the alias table unchanged:

```go
type Selector struct {
	targets       []config.TargetConfig
	templateNames [][]string
	sequences     []atomic.Uint64
	alias         []int
	prob          []float64
	n             int
}
```

During construction, collect unique names from all supported fields with `config.TemplateVariables`. Return a target-indexed error for malformed templates or unknown names so programmatically constructed selectors remain safe.

- [ ] **Step 4: Expand a copied target after selection**

Keep alias selection intact, then route the chosen index through:

```go
func (s *Selector) taskAt(index int) Task
```

Select one random candidate for each referenced custom variable. Generate built-ins only when referenced. Use `atomic.Uint64.Add(1)` for `seq`, `time.Now().Unix()` for `timestamp`, and this standard-library UUIDv4 shape:

```go
func randomUUID() string {
	var value [16]byte
	if _, err := cryptorand.Read(value[:]); err != nil {
		panic(fmt.Errorf("generating request UUID: %w", err))
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
```

Copy the selected `TargetConfig`; replace URL and body strings; allocate a new WebSocket message slice before replacing each message. Do not mutate `s.targets`, candidate maps, or source slices.

- [ ] **Step 5: Provide dry-run examples without a second expansion path**

Add:

```go
func (s *Selector) Examples() []Task {
	tasks := make([]Task, len(s.targets))
	for i := range s.targets {
		tasks[i] = s.taskAt(i)
	}
	return tasks
}
```

Dry-run creates its own selector, so consuming sequence `1` in that short-lived selector does not alter runtime behavior.

- [ ] **Step 6: Extend fuzz and concurrency coverage**

Seed `internal/task/task_fuzz_test.go` with replacement values containing braces, `$`, backslashes, JSON, query delimiters, and Unicode. Extend `TestPick_ConcurrentSafety` to use `{{seq}}`, collect values, and assert uniqueness; run it with `-race`.

- [ ] **Step 7: Run unit, race, and benchmark checks**

Run: `go test ./internal/task`

Run: `go test -race ./internal/task`

Run: `go test -run '^$' -bench BenchmarkSelector ./internal/task`

Expected: all tests PASS; benchmark compiles and weighted selection remains O(1).

- [ ] **Step 8: Commit the selector unit**

```bash
git add internal/task/task.go internal/task/task_test.go internal/task/task_fuzz_test.go internal/task/selector_bench_test.go
git commit -m "feat: expand request templates per dispatch"
```

### Task 3: Dry-Run And Dispatch Integration

**Files:**
- Modify: `cmd/sendit/helpers.go:17-78`
- Modify: `cmd/sendit/start.go:56-101`
- Modify: `cmd/sendit/validate.go:18-32`
- Modify: `cmd/sendit/main_test.go:582-686`
- Modify: `cmd/sendit/integration_test.go:100-122`
- Modify: `internal/engine/integration_test.go`

**Interfaces:**
- Consumes: `task.NewSelector` and `(*task.Selector).Examples` from Task 2.
- Produces: `printDryRun(path string, cfg *config.Config, duration time.Duration) error`; engine and driver interfaces remain unchanged.

- [ ] **Step 1: Add failing dry-run tests**

Change existing dry-run tests to capture the returned error. Add a templated target and assert output contains an expanded URL, not raw delimiters:

```go
func TestPrintDryRunExpandsExampleURL(t *testing.T) {
	cfg := makeDryRunConfig("human")
	cfg.Targets = []config.TargetConfig{{
		URL: "https://example.com/users/{{user}}/{{seq}}",
		Type: "http", Weight: 1,
		Vars: map[string][]string{"user": {"alice"}},
	}}
	out := captureStdout(t, func() {
		if err := printDryRun("config/test.yaml", cfg, 0); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "https://example.com/users/alice/1") || strings.Contains(out, "{{") {
		t.Fatalf("unexpected dry-run output: %q", out)
	}
}
```

- [ ] **Step 2: Add a failing HTTP dispatch integration test**

In `internal/engine/integration_test.go`, start an `httptest.Server`, configure one target whose URL path and HTTP body share `{{name}}` and whose URL uses `{{seq}}`, run the engine until the observer sees a result, and assert the server received the expanded path and matching body. This demonstrates expansion occurs before both hostname extraction and driver execution.

- [ ] **Step 3: Run focused tests and confirm they fail**

Run: `go test ./cmd/sendit ./internal/engine -run 'TestPrintDryRunExpands|TestIntegrationRequestTemplate'`

Expected: FAIL because dry-run and dispatch currently expose raw placeholders.

- [ ] **Step 4: Reuse selector expansion in dry-run**

Change `printDryRun` to return an error. Construct a selector from the sorted targets, call `Examples`, and print each example URL while retaining the configured type, weight, and share. Return selector construction errors. In `start.go`, use:

```go
if dryRun {
	return printDryRun(cfgPath, cfg, duration)
}
```

Do not print expanded bodies, variables, or credentials.

- [ ] **Step 5: Update CLI help and validation descriptions**

In `start.go`, document `vars`, mapped newline `vars_file`, built-ins, and the four expansion surfaces. Correct the existing target type list to `http | browser | dns | websocket | grpc | sftp`. In `validate.go`, state that variable files and placeholders are checked.

- [ ] **Step 6: Run CLI and integration tests**

Run: `go test ./cmd/sendit ./internal/engine`

Run: `make integration`

Expected: PASS.

- [ ] **Step 7: Commit the user-visible behavior**

```bash
git add cmd/sendit/helpers.go cmd/sendit/start.go cmd/sendit/validate.go cmd/sendit/main_test.go cmd/sendit/integration_test.go internal/engine/integration_test.go
git commit -m "feat: show and dispatch expanded requests"
```

### Task 4: Documentation, Release Artifacts, And Verification

**Files:**
- Modify: `config/example.yaml`
- Modify: `README.md`
- Modify: `docs/content/docs/configuration.md`
- Modify: `docs/content/docs/drivers.md`
- Modify: `docs/content/docs/cli.md`
- Modify: `CHANGELOG.md`
- Modify: `ROADMAP.md`

**Interfaces:**
- Consumes: final syntax and behavior from Tasks 1-3.
- Produces: complete user documentation and release metadata for issue #251.

- [ ] **Step 1: Add the complete example configuration**

Add one target to `config/example.yaml` demonstrating inline candidates, a mapped newline file, URL/body substitution, and all built-ins. Comments must state that paths are relative to the main YAML and that expansion is single-pass.

- [ ] **Step 2: Document configuration semantics**

Add matching sections to `README.md` and `docs/content/docs/configuration.md` covering:

```yaml
vars:
  user_id: [alice, bob]
vars_file:
  region: data/regions.txt
```

Document the identifier grammar, reserved built-ins, per-request uniform selection, sequence reset behavior, file trimming/blank-line behavior, duplicate rejection, relative paths, validation errors, and `target_defaults` inheritance.

- [ ] **Step 3: Document driver surfaces and CLI output**

Update `docs/content/docs/drivers.md` to identify URL, HTTP body, gRPC body, and WebSocket messages as the only supported surfaces. Update `docs/content/docs/cli.md` with template validation behavior and a dry-run example showing an expanded URL.

- [ ] **Step 4: Update release tracking**

Under `[Unreleased]` in `CHANGELOG.md`, add an `Added` entry for per-request custom/file/built-in templating. Mark the request-templating roadmap item complete and replace the old CSV-or-newline wording with the mapped newline-file behavior approved for #251.

- [ ] **Step 5: Check documentation references**

Run: `rg -n "vars_file|request templating|\{\{uuid\}\}|CSV or newline" README.md config/example.yaml docs/content/docs CHANGELOG.md ROADMAP.md`

Expected: syntax and semantics agree across all user-facing files; no stale promise of CSV support remains.

- [ ] **Step 6: Run formatting and full verification**

Run: `gofmt -w internal/config/schema.go internal/config/config.go internal/config/template.go internal/config/config_test.go internal/config/config_fuzz_test.go internal/task/task.go internal/task/task_test.go internal/task/task_fuzz_test.go internal/task/selector_bench_test.go cmd/sendit/helpers.go cmd/sendit/start.go cmd/sendit/validate.go cmd/sendit/main_test.go cmd/sendit/integration_test.go internal/engine/integration_test.go`

Run: `GOLANGCI_LINT_CACHE=/tmp/opencode/golangci-cache-request-templating make verify GOLANGCI_LINT=/tmp/opencode/bin/golangci-lint`

Expected: build, lint, race-enabled unit tests, and integration tests all PASS.

- [ ] **Step 7: Update generated architecture graph when present**

If `graphify-out/graph.json` exists, run `graphify update .`. Do not stage ignored generated graph output.

- [ ] **Step 8: Commit documentation and release metadata**

```bash
git add config/example.yaml README.md docs/content/docs/configuration.md docs/content/docs/drivers.md docs/content/docs/cli.md CHANGELOG.md ROADMAP.md docs/superpowers/specs/2026-10-08-request-templating-design.md docs/superpowers/plans/2026-10-08-request-templating.md
git commit -m "docs: document request templating"
```

- [ ] **Step 9: Review, push, and open the linked pull request**

Inspect `git status`, `git diff origin/main...HEAD`, and recent commits. Push with authenticated HTTPS, open one focused PR containing `Closes #251`, and report the `make verify` result in the PR body.
