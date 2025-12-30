package app

import "fmt"

// View renders the current application state into a string for terminal display.
// Different screens (auth, inbox, detail, search) have different layouts and controls.
// Returns the formatted string to be displayed by Bubble Tea.
func (m model) View() string {
	title := bold.Render("Gmail TUI")
	if m.err != nil {
		return pad.Render(box.Render(title+"\n\nError: "+m.err.Error()+"\n\n"+faint.Render("q quit"))) + "\n"
	}

	switch m.screen {
	case screenAuth:
		body := "No saved token found.\n\nPress l to login in your browser.\n\n" + faint.Render("l login • q quit")
		return pad.Render(box.Render(title+"\n\n"+body)) + "\n"

	case screenSearch:
		body := "Search\n\n" + m.searchInput.View() + "\n\n" + faint.Render("enter apply • esc cancel")
		return pad.Render(box.Render(title+"\n\n"+body)) + "\n"

	case screenInbox:
		h := title + "\n" + faint.Render("enter open • / search • g labels • r refresh • q quit")
		if m.query != "" {
			h += "\n" + fmt.Sprintf("Query: %s", m.query)
		}
		return pad.Render(box.Render(h+"\n\n"+m.inbox.View())) + "\n"

	case screenDetail:
		h := title + "\n" + faint.Render("b back • r reload • q quit")
		return pad.Render(box.Render(h+"\n\n"+m.detailVP.View())) + "\n"

	case screenLabels:
		h := title + "\n" + faint.Render("enter filter by label • b back • r refresh • q quit")
		return pad.Render(box.Render(h+"\n\n"+m.labels.View())) + "\n"
	}

	return ""
}
