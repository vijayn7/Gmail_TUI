package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"gmail-tui/internal/app"
)

func main() {
	p := tea.NewProgram(app.NewModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
