---
title: "CLI Reference"
linkTitle: "CLI"
weight: 6
description: "All sendit commands and their flags."
---

## Commands

```
sendit start    [-c <path>] [--foreground] [--log-level debug|info|warn|error] [--dry-run]
sendit probe    <target>   [--type http|dns] [--interval 1s] [--timeout 5s]
sendit stop     [--pid-file <path>]
sendit reload   [--pid-file <path>]
sendit status   [--pid-file <path>]
sendit validate [-c <path>]
sendit version
sendit completion <shell>
```

| Command | Description |
|---|---|
| `start` | Start the engine. Writes a PID file by default so `stop`/`status` can find the process; use `--foreground` to skip. |
| `probe` | Test a single HTTP or DNS endpoint in a loop (like ping). No config file needed. |
| `stop` | Send SIGTERM to the running instance via its PID file. Waits for in-flight requests to finish. |
| `reload` | Send SIGHUP to the running instance via its PID file to hot-reload config atomically. |
| `status` | Report whether the process in the PID file is still alive. |
| `validate` | Parse and validate a config file. Exits 0 on success, non-zero with a message on error. |
| `version` | Print version, commit hash, and build date. |
| `completion` | Generate shell autocompletion scripts for bash, zsh, fish, or powershell. |

## `start` flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | `-c` | `config/example.yaml` | Path to YAML config file |
| `--foreground` | | `false` | Skip writing the PID file |
| `--log-level` | | *(from config)* | Override log level: `debug` \| `info` \| `warn` \| `error` |
| `--dry-run` | | `false` | Print config summary and exit without sending traffic |

### Dry-run output example

```sh
./sendit start --config config/example.yaml --dry-run
```

```
Config: config/example.yaml  ✓ valid

Targets (4):
  URL                                      TYPE       WEIGHT     SHARE
  https://httpbin.org/get                  http       10         47.6%
  https://httpbin.org/status/200           http       5          23.8%
  https://news.ycombinator.com             browser    3          14.3%
  example.com                              dns        3          14.3%
  Total weight: 21

Pacing:
  mode: human | delay: 800ms–8000ms (random uniform)

Limits:
  workers: 4 (browser: 1) | cpu: 60% | memory: 512 MB
```

## `probe` flags

| Flag | Default | Description |
|---|---|---|
| `--type` | *(auto-detected)* | Driver type: `http` \| `dns` |
| `--interval` | `1s` | Delay between requests |
| `--timeout` | `5s` | Per-request timeout |
| `--resolver` | `8.8.8.8:53` | DNS resolver (dns targets only) |
| `--record-type` | `A` | DNS record type (dns targets only) |

**Auto-detection rules:**

| Target format | Detected type |
|---|---|
| `https://example.com` | `http` |
| `http://example.com` | `http` |
| `example.com` | `dns` |

### HTTP probe example

```sh
./sendit probe https://example.com
```

```
Probing https://example.com (http) — Ctrl-C to stop

  200   142ms  1.2 KB
  200    38ms  1.2 KB
^C

--- https://example.com ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 38ms / 90ms / 142ms
```

### DNS probe example

```sh
./sendit probe example.com --record-type A --resolver 1.1.1.1:53
```

```
Probing example.com (dns, A @ 1.1.1.1:53) — Ctrl-C to stop

  NOERROR    12ms
  NOERROR     8ms
^C

--- example.com ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 8ms / 10ms / 12ms
```

## `stop` / `reload` / `status` flags

| Flag | Default | Description |
|---|---|---|
| `--pid-file` | `/tmp/sendit.pid` | Path to PID file written by `start` |

## `validate` flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | `-c` | `config/example.yaml` | Path to YAML config file |

## Shell completion

```sh
# bash (add to ~/.bashrc)
source <(./sendit completion bash)

# zsh (add to ~/.zshrc)
source <(./sendit completion zsh)

# fish
./sendit completion fish | source
```
