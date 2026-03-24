// Package tui implements the optional Bubble Tea terminal UI for sendit start.
//
// It is activated with --tui on a TTY. When stdout is not a terminal the flag
// is silently ignored and plain zerolog output continues unchanged.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lewta/sendit/internal/config"
)

// Run starts the Bubble Tea program and blocks until the user quits (q /
// ctrl-c) or ctx is cancelled (SIGINT / --duration timeout). When ctx is
// cancelled the program is asked to quit and Run returns nil.
func Run(ctx context.Context, state *State, cfg *config.Config) error {
	p := tea.NewProgram(
		newModel(state, cfg),
		tea.WithAltScreen(),
	)

	errCh := make(chan error, 1)
	go func() {
		_, err := p.Run()
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		p.Quit()
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}
