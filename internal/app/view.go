package app

import "fmt"

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
		h := title + "\n" + faint.Render("enter open • / search • r refresh • q quit")
		if m.query != "" {
			h += "\n" + fmt.Sprintf("Query: %s", m.query)
		}
		return pad.Render(box.Render(h+"\n\n"+m.inbox.View())) + "\n"

	case screenDetail:
		h := title + "\n" + faint.Render("b back • r reload • q quit")
		return pad.Render(box.Render(h+"\n\n"+m.detailVP.View())) + "\n"
	}

	return ""
}
