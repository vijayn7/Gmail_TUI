package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"gmail-tui/internal/app"
)

// main initializes and runs the Gmail TUI application using the Bubble Tea framework.
// It creates a new program with an alternate screen buffer (fullscreen mode) and handles any startup errors.
func main() {
	p := tea.NewProgram(app.NewModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
