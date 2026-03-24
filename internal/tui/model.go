package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lewta/sendit/internal/config"
)

const tickInterval = 100 * time.Millisecond

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	labelStyle  = lipgloss.NewStyle().Bold(true).Width(10)
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dimStyle    = lipgloss.NewStyle().Faint(true)
)

// sparkChars are Unicode block elements ordered from shortest to tallest.
var sparkChars = []rune("▁▂▃▄▅▆▇█")

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type model struct {
	state *State
	cfg   *config.Config
	width int
}

func newModel(state *State, cfg *config.Config) model {
	return model{state: state, cfg: cfg, width: 80}
}

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tickMsg:
		return m, tick()
	}
	return m, nil
}

func (m model) View() string {
	sn := m.state.Snapshot()

	var sb strings.Builder

	sb.WriteString(headerStyle.Render("sendit") + dimStyle.Render(" — q or ctrl-c to stop\n\n"))

	sb.WriteString(labelStyle.Render("Mode") + modeDesc(m.cfg) + "\n")
	sb.WriteString(labelStyle.Render("Running") + formatElapsed(sn.Elapsed) + "\n\n")

	errPct := ""
	if sn.Total > 0 {
		errPct = fmt.Sprintf(" (%.1f%%)", float64(sn.Errors)/float64(sn.Total)*100)
	}
	sb.WriteString(labelStyle.Render("Requests") +
		fmt.Sprintf("%s total · %s ok · %s errors%s\n",
			formatInt(sn.Total),
			okStyle.Render(formatInt(sn.Success)),
			errStyle.Render(formatInt(sn.Errors)),
			dimStyle.Render(errPct),
		))

	avg := sn.Avg()
	p95 := sn.P95()
	if avg > 0 {
		sb.WriteString(labelStyle.Render("Latency") +
			fmt.Sprintf("avg %s · p95 %s\n",
				avg.Round(time.Millisecond),
				p95.Round(time.Millisecond),
			))
	} else {
		sb.WriteString(labelStyle.Render("Latency") + dimStyle.Render("waiting…\n"))
	}

	if len(sn.Latencies) > 1 {
		maxW := m.width - 12
		if maxW < 10 {
			maxW = 10
		}
		spark := sparkline(sn.Latencies, maxW)
		sb.WriteString("\n" + strings.Repeat(" ", 10) + dimStyle.Render(spark) + "\n")
	}

	return sb.String()
}

func modeDesc(cfg *config.Config) string {
	p := cfg.Pacing
	switch p.Mode {
	case "rate_limited":
		return fmt.Sprintf("rate_limited · %.0f rpm · %d workers", p.RequestsPerMinute, cfg.Limits.MaxWorkers)
	case "human":
		return fmt.Sprintf("human · %d–%dms delay · %d workers", p.MinDelayMs, p.MaxDelayMs, cfg.Limits.MaxWorkers)
	case "scheduled":
		return fmt.Sprintf("scheduled · %.0f rpm (in-window) · %d workers", p.RequestsPerMinute, cfg.Limits.MaxWorkers)
	case "burst":
		return fmt.Sprintf("burst · %d workers", cfg.Limits.MaxWorkers)
	default:
		return p.Mode
	}
}

func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var buf strings.Builder
	mod := len(s) % 3
	if mod == 0 {
		mod = 3
	}
	buf.WriteString(s[:mod])
	for i := mod; i < len(s); i += 3 {
		buf.WriteByte(',')
		buf.WriteString(s[i : i+3])
	}
	return buf.String()
}

// sparkline renders lats as a row of Unicode block characters scaled to the
// min/max of the sample. Width limits the number of characters rendered.
func sparkline(lats []time.Duration, maxWidth int) string {
	n := len(lats)
	if n > maxWidth {
		lats = lats[n-maxWidth:]
		n = maxWidth
	}
	if n < 2 {
		return ""
	}

	min, max := int64(lats[0]), int64(lats[0])
	for _, l := range lats[1:] {
		v := int64(l)
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	runes := make([]rune, n)
	if max == min {
		for i := range runes {
			runes[i] = sparkChars[0]
		}
	} else {
		for i, l := range lats {
			v := float64(int64(l)-min) / float64(max-min)
			idx := int(v * float64(len(sparkChars)-1))
			runes[i] = sparkChars[idx]
		}
	}
	return string(runes)
}
