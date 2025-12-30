package app

import (
	"context"

	"gmail-tui/internal/auth"
	gmailx "gmail-tui/internal/gmail"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/oauth2"
)

type oauthCfgMsg struct {
	cfg *oauth2ConfigWrap
	err error
}

type oauth2ConfigWrap struct {
	cfg any
}

type loadedCfgMsg struct {
	cfg *OAuthConfig
	err error
}

type OAuthConfig struct {
	Cfg interface{}
}

type tokenMsg struct {
	tok any
	err error
}

type inboxMsg struct {
	items []list.Item
	err   error
}

type detailMsg struct {
	content string
	err     error
}

type labelsMsg struct {
	items []list.Item
	err   error
}

type loginDoneMsg struct {
	err error
}

// Init initializes the application by loading OAuth configuration and saved tokens.
// This is called once when the Bubble Tea program starts. Returns a batch command
// that executes both loading operations in parallel.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadCfgCmd(), m.loadTokenCmd())
}

// loadCfgCmd creates a command that loads the OAuth configuration from credentials.json.
// Returns a cfgMsg with the configuration on success, or an errMsg on failure.
func (m model) loadCfgCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := loadOAuthConfig()
		if err != nil {
			return errMsg{err: err}
		}
		return cfgMsg{cfg: cfg}
	}
}

type cfgMsg struct {
	cfg *oauth2.Config
}

type errMsg struct {
	err error
}

// loadTokenCmd creates a command that loads a previously saved OAuth token from disk.
// Returns a tokenLoadedMsg with the token if found, or nil if no token exists.
// This allows automatic login without requiring user authentication each time.
func (m model) loadTokenCmd() tea.Cmd {
	return func() tea.Msg {
		if m.store == nil {
			return tokenLoadedMsg{tok: nil, err: nil}
		}
		tok, err := m.store.Load()
		if err != nil {
			return tokenLoadedMsg{tok: nil, err: err}
		}
		return tokenLoadedMsg{tok: tok, err: nil}
	}
}

type tokenLoadedMsg struct {
	tok *oauth2.Token
	err error
}

// loginCmd initiates the OAuth2 login flow using a local loopback server.
// Opens the user's browser to Google's authentication page, waits for authorization,
// and saves the resulting token to disk for future use.
func (m model) loginCmd() tea.Cmd {
	return func() tea.Msg {
		if m.cfg == nil {
			return errMsg{err: errMissingCfg{}}
		}
		tok, err := auth.LoopbackLogin(m.cfg)
		if err != nil {
			return loginDoneMsg{err: err}
		}
		if m.store != nil {
			_ = m.store.Save(tok)
		}
		return tokenLoadedMsg{tok: tok, err: nil}
	}
}

type errMissingCfg struct{}

// Error returns the error message for missing OAuth configuration.
func (e errMissingCfg) Error() string { return "missing oauth config" }

// fetchInboxCmd creates a command that fetches up to 25 emails from the Gmail inbox.
// Uses the current search query if one is set. Converts Gmail API responses into
// list items for display in the TUI. Has a 20-second timeout for the API call.
func (m model) fetchInboxCmd() tea.Cmd {
	cfg := m.cfg
	tok := m.token
	q := m.query

	return func() tea.Msg {
		if cfg == nil || tok == nil {
			return inboxMsg{err: errMissingCfg{}}
		}
		ctx, cancel := gmailx.HumanTimeoutCtx(context.Background(), 20)
		defer cancel()

		c, err := gmailx.New(ctx, cfg, tok)
		if err != nil {
			return inboxMsg{err: err}
		}
		rows, err := c.ListInbox(ctx, 25, q)
		if err != nil {
			return inboxMsg{err: err}
		}
		items := make([]list.Item, 0, len(rows))
		for _, r := range rows {
			items = append(items, emailItem{
				id:      r.ID,
				subject: r.Subject,
				from:    r.From,
				date:    r.Date,
				snippet: r.Snippet,
			})
		}
		return inboxMsg{items: items, err: nil}
	}
}

// fetchDetailCmd creates a command that fetches the full details of a specific email by ID.
// Formats the email headers and body into a readable string for display in the detail view.
// Has a 20-second timeout for the API call.
func (m model) fetchDetailCmd(id string) tea.Cmd {
	cfg := m.cfg
	tok := m.token

	return func() tea.Msg {
		ctx, cancel := gmailx.HumanTimeoutCtx(context.Background(), 20)
		defer cancel()

		c, err := gmailx.New(ctx, cfg, tok)
		if err != nil {
			return detailMsg{err: err}
		}
		d, err := c.GetDetail(ctx, id)
		if err != nil {
			return detailMsg{err: err}
		}
		content := ""
		content += "Subject: " + d.Subject + "\n"
		content += "From:    " + d.From + "\n"
		if d.To != "" {
			content += "To:      " + d.To + "\n"
		}
		if d.Date != "" {
			content += "Date:    " + d.Date + "\n"
		}
		content += "\nSnippet:\n" + d.Snippet + "\n"
		content += "\nBody:\n" + d.Body + "\n"
		return detailMsg{content: content, err: nil}
	}
}

// fetchLabelsCmd creates a command that fetches all Gmail labels for the user's account.
// Labels include both system labels (INBOX, SENT, TRASH, etc.) and custom user-created labels.
// Has a 20-second timeout for the API call.
func (m model) fetchLabelsCmd() tea.Cmd {
	cfg := m.cfg
	tok := m.token

	return func() tea.Msg {
		if cfg == nil || tok == nil {
			return labelsMsg{err: errMissingCfg{}}
		}
		ctx, cancel := gmailx.HumanTimeoutCtx(context.Background(), 20)
		defer cancel()

		c, err := gmailx.New(ctx, cfg, tok)
		if err != nil {
			return labelsMsg{err: err}
		}
		labels, err := c.ListLabels(ctx)
		if err != nil {
			return labelsMsg{err: err}
		}
		items := make([]list.Item, 0, len(labels))
		for _, label := range labels {
			items = append(items, labelItem{
				id:   label.ID,
				name: label.Name,
			})
		}
		return labelsMsg{items: items, err: nil}
	}
}

// Update handles all incoming messages and updates the application state accordingly.
// This is the main event handler that processes window resizes, keyboard input,
// and async command results. Returns the updated model and any new commands to execute.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.inbox.SetSize(msg.Width-6, msg.Height-10)
		m.labels.SetSize(msg.Width-6, msg.Height-10)
		m.detailVP.Width = msg.Width - 6
		m.detailVP.Height = msg.Height - 10
		return m, nil

	case cfgMsg:
		m.cfg = msg.cfg
		return m, nil

	case tokenLoadedMsg:
		if msg.tok != nil && msg.err == nil {
			m.token = msg.tok
			m.screen = screenInbox
			m.status = "Logged in"
			return m, m.fetchInboxCmd()
		}
		return m, nil

	case inboxMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.inbox.SetItems(msg.items)
		return m, nil

	case detailMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.detailVP.SetContent(msg.content)
		m.screen = screenDetail
		return m, nil

	case labelsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.labels.SetItems(msg.items)
		m.screen = screenLabels
		return m, nil

	case loginDoneMsg:
		m.err = msg.err
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		k := msg.String()

		if k == "ctrl+c" || k == "q" {
			return m, tea.Quit
		}

		switch m.screen {
		case screenAuth:
			if k == "l" {
				m.err = nil
				m.status = "Opening browser for login..."
				return m, m.loginCmd()
			}
			return m, nil

		case screenInbox:
			switch k {
			case "r":
				return m, m.fetchInboxCmd()
			case "g":
				return m, m.fetchLabelsCmd()
			case "/":
				m.searchInput.SetValue(m.query)
				m.searchInput.Focus()
				m.screen = screenSearch
				return m, nil
			case "enter":
				if it, ok := m.inbox.SelectedItem().(emailItem); ok {
					m.detailID = it.id
					m.status = "Loading message..."
					return m, m.fetchDetailCmd(it.id)
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.inbox, cmd = m.inbox.Update(msg)
			return m, cmd

		case screenDetail:
			switch k {
			case "b":
				m.screen = screenInbox
				return m, nil
			case "r":
				if m.detailID != "" {
					return m, m.fetchDetailCmd(m.detailID)
				}
			}
			var cmd tea.Cmd
			m.detailVP, cmd = m.detailVP.Update(msg)
			return m, cmd

		case screenSearch:
			switch k {
			case "esc":
				m.screen = screenInbox
				m.searchInput.Blur()
				return m, nil
			case "enter":
				m.query = m.searchInput.Value()
				m.searchInput.Blur()
				m.screen = screenInbox
				return m, m.fetchInboxCmd()
			}
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd

		case screenLabels:
			switch k {
			case "b":
				m.screen = screenInbox
				return m, nil
			case "r":
				return m, m.fetchLabelsCmd()
			case "enter":
				if it, ok := m.labels.SelectedItem().(labelItem); ok {
					// Use label ID for filtering - Gmail search uses label IDs
					m.query = "label:" + it.id
					m.screen = screenInbox
					return m, m.fetchInboxCmd()
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.labels, cmd = m.labels.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}
