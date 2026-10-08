# Request Templating Design

## Goal

Allow one target definition to generate varied requests by expanding per-request variables in the target URL, HTTP body, gRPC body, and WebSocket send messages.

Issue [#251](https://github.com/lewta/sendit/issues/251) is the canonical tracker.

## Configuration

Targets may define inline candidate values and newline-delimited value files:

```yaml
targets:
  - url: https://api.example.com/users/{{user_id}}?request={{uuid}}
    type: http
    weight: 10
    vars:
      user_id: [alice, bob]
    vars_file:
      region: data/regions.txt
    http:
      method: POST
      body: '{"user":"{{user_id}}","region":"{{region}}","sequence":{{seq}}}'
```

`vars_file` is a map from variable name to a newline-delimited file. Relative paths resolve from the directory containing the main YAML configuration. Values are trimmed; blank lines are ignored. An empty file is invalid.

Variable names use lowercase snake-case identifiers: `[a-z][a-z0-9_]*`. The names `uuid`, `timestamp`, and `seq` are reserved. A name cannot appear in both `vars` and `vars_file` on the same effective target.

`target_defaults.vars` and `target_defaults.vars_file` apply to targets loaded through `targets_file`, matching the existing defaults behavior.

## Expansion Semantics

Expansion occurs after weighted target selection and before resource admission, domain rate limiting, and driver execution. Every supported field in one dispatched task uses the same selected/generated value for a given variable.

Custom variables choose one candidate uniformly per request. Built-ins are:

- `{{uuid}}`: random RFC 4122 UUIDv4.
- `{{timestamp}}`: Unix epoch seconds at expansion time.
- `{{seq}}`: per-target counter starting at 1.

Sequence counters are independent for each target entry and reset when the process starts or a configuration reload creates a new selector. Selection and sequencing remain safe if `Pick` is called concurrently.

Substitution is a single pass. Candidate values containing template syntax are inserted literally and are not expanded recursively.

Supported fields are:

- `target.url`
- `http.body`
- `grpc.body`
- Each `websocket.send_messages` entry

Headers, authentication values, browser options, DNS options, and SFTP options are outside this change.

## Validation And Errors

Configuration loading fails before startup or reload when:

- A variable name is empty, invalid, or reserved.
- An inline candidate list is empty or contains an empty value.
- A variable-file path is empty, unreadable, or contains no non-blank values.
- A variable appears in both `vars` and `vars_file`.
- A supported field contains malformed template delimiters.
- A placeholder is neither a defined custom variable nor a built-in.

Diagnostics identify the target and field. Map-backed errors are sorted for deterministic output. Failed reloads keep the previous runtime configuration and selector.

## Runtime Design

`internal/config` owns template syntax parsing, variable-file loading, and validation because `config.Load` must reject invalid configuration independently of the engine. The task selector reuses those helpers and stores the referenced names and an atomic sequence counter per target.

`Selector.Pick` keeps weighted selection unchanged, creates one value map for the selected target, and expands a copied `TargetConfig`. It copies the WebSocket message slice before replacing values, so source configuration is never mutated. Drivers continue consuming ordinary fully expanded task fields.

The selector also provides one expanded example per configured target for dry-run output. Dry-run shows expanded URLs only; it does not print bodies, variable maps, or authentication data.

## Documentation And Testing

Unit tests cover parsing, validation, relative file loading, built-ins, literal one-pass replacement, per-target sequencing, cross-field consistency, source immutability, and concurrent selection. Integration coverage confirms that the HTTP driver receives an expanded URL and body. CLI tests cover expanded dry-run output.

The implementation updates CLI help, `config/example.yaml`, `README.md`, Hugo configuration/driver/CLI pages, `CHANGELOG.md`, and `ROADMAP.md` in the same pull request.
