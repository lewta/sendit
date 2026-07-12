---
title: "Dependencies"
linkTitle: "Dependencies"
weight: 95
description: "Direct dependencies, their purpose, and their licences."
---

sendit has 19 direct runtime dependencies and 1 direct test dependency. All are permissive open-source licences
compatible with the project's [MIT licence](https://github.com/lewta/sendit/blob/main/LICENSE).

The module graph is managed with `go mod tidy` and kept minimal — no dependency
appears that cannot be justified by the table below.

## Direct dependencies

| Module | Version | Licence | Purpose |
|--------|---------|---------|---------|
| [`github.com/charmbracelet/bubbletea`](https://github.com/charmbracelet/bubbletea) | v1.3.10 | MIT | Elm-architecture TUI framework — powers the `--tui` terminal dashboard |
| [`github.com/charmbracelet/lipgloss`](https://github.com/charmbracelet/lipgloss) | v1.1.0 | MIT | Style definitions for the terminal UI (bold labels, colour-coded counters) |
| [`github.com/chromedp/chromedp`](https://github.com/chromedp/chromedp) | v0.15.1 | MIT | Browser automation via the Chrome DevTools Protocol — powers the `browser` driver |
| [`github.com/coder/websocket`](https://github.com/coder/websocket) | v1.8.15 | ISC | WebSocket client — powers the `websocket` driver |
| [`github.com/miekg/dns`](https://github.com/miekg/dns) | v1.1.72 | BSD-3-Clause | Full-featured DNS client and server library — powers the `dns` driver |
| [`github.com/pkg/sftp`](https://github.com/pkg/sftp) | v1.13.11 | BSD-2-Clause | SFTP client and test server — powers the `sftp` driver |
| [`github.com/prometheus/client_golang`](https://github.com/prometheus/client_golang) | v1.23.2 | Apache-2.0 | Prometheus metrics exposition (`/metrics` endpoint) |
| [`github.com/robfig/cron/v3`](https://github.com/robfig/cron) | v3.0.1 | MIT | Cron expression parser — used by `scheduled` pacing mode to define active windows |
| [`github.com/rs/zerolog`](https://github.com/rs/zerolog) | v1.35.1 | MIT | Zero-allocation structured logger; `zerolog.Nop()` used internally for no-op metrics |
| [`github.com/shirou/gopsutil/v3`](https://github.com/shirou/gopsutil) | v3.24.5 | BSD-3-Clause | Cross-platform CPU and memory utilisation polling — powers the resource admission gate |
| [`github.com/spf13/cobra`](https://github.com/spf13/cobra) | v1.10.2 | Apache-2.0 | CLI framework — commands, flags, and shell completion generation |
| [`github.com/spf13/viper`](https://github.com/spf13/viper) | v1.21.0 | MIT | Config file loading with environment variable overlay and `mapstructure` unmarshalling |
| [`golang.org/x/crypto`](https://pkg.go.dev/golang.org/x/crypto) | v0.54.0 | BSD-3-Clause | `ssh` subpackage — SSH transport and algorithm policy controls for the `sftp` driver |
| [`golang.org/x/net`](https://pkg.go.dev/golang.org/x/net) | v0.57.0 | BSD-3-Clause | `html` subpackage — HTML parser used by the `generate` command to extract links |
| [`golang.org/x/time`](https://pkg.go.dev/golang.org/x/time) | v0.15.0 | BSD-3-Clause | `rate` subpackage — token-bucket rate limiter used by `rate_limited` and `scheduled` pacing |
| [`google.golang.org/grpc`](https://pkg.go.dev/google.golang.org/grpc) | v1.82.0 | Apache-2.0 | gRPC client and server — powers the `grpc` driver; includes reflection client and health service |
| [`google.golang.org/protobuf`](https://pkg.go.dev/google.golang.org/protobuf) | v1.36.11 | BSD-3-Clause | Dynamic protobuf messages and JSON/protobuf marshaling for the reflection-based `grpc` driver |
| [`howett.net/plist`](https://pkg.go.dev/howett.net/plist) | v1.0.1 | BSD-2-Clause | Property-list parser used by `generate` to read Safari bookmarks |
| [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) | v1.53.0 | BSD-3-Clause | Pure-Go SQLite driver (CGo-free) — used by `generate` to read Chrome/Firefox history and bookmark databases |

## Test dependencies

| Module | Version | Licence | Purpose |
|--------|---------|---------|---------|
| [`github.com/cucumber/godog`](https://github.com/cucumber/godog) | v0.15.1 | MIT | Cucumber/Gherkin BDD test framework — powers the `features/auth.feature` behavioural tests for the auth block |

## Alternatives considered

The following alternatives were evaluated during the v0.13.3 audit and ruled out:

| Dependency | Alternative considered | Decision |
|------------|----------------------|----------|
| `golang.org/x/net/html` | stdlib | No stdlib equivalent — `x/net/html` is the canonical Go HTML parser |
| `golang.org/x/time/rate` | stdlib | No stdlib token-bucket; `time.Ticker` cannot express burst semantics |
| `golang.org/x/crypto/ssh` | stdlib | No SSH client in the standard library; `x/crypto/ssh` is the maintained Go SSH implementation |
| `github.com/pkg/sftp` | hand-rolled SFTP packets | Reusing the established SFTP client avoids implementing protocol framing and extension handling locally |
| `github.com/spf13/viper` | stdlib `os.Getenv` + manual YAML | Viper provides env-override, defaults, and `mapstructure` in one — justified for config complexity |
| `github.com/rs/zerolog` | stdlib `log/slog` (Go 1.21+) | zerolog's zero-allocation design and `Nop()` no-op are better suited for high-throughput dispatch paths |

## Licence compatibility

All dependency licences are permissive and compatible with the project's MIT licence:

| Licence | Dependencies |
|---------|-------------|
| MIT | `bubbletea`, `lipgloss`, `chromedp`, `cron/v3`, `zerolog`, `viper` |
| ISC | `coder/websocket` |
| BSD-2-Clause | `pkg/sftp`, `howett.net/plist` |
| BSD-3-Clause | `miekg/dns`, `gopsutil/v3`, `x/crypto`, `x/net`, `x/time`, `google.golang.org/protobuf`, `modernc.org/sqlite` |
| Apache-2.0 | `prometheus/client_golang`, `cobra`, `google.golang.org/grpc` |

ISC, BSD-2-Clause, and BSD-3-Clause are functionally equivalent to MIT for distribution purposes.
Apache-2.0 is compatible with MIT when distributing binaries (no copyleft restriction).
