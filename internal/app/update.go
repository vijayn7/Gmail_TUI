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

type loginDoneMsg struct {
	err error
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadCfgCmd(), m.loadTokenCmd())
}

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

func (e errMissingCfg) Error() string { return "missing oauth config" }

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

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.inbox.SetSize(msg.Width-6, msg.Height-10)
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
		}
	}

	return m, nil
}
