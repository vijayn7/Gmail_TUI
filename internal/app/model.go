package app

import (
	"errors"
	"os"

	"gmail-tui/internal/store"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const credentialsFile = "credentials.json"

const gmailReadonlyScope = "https://www.googleapis.com/auth/gmail.readonly"

type screen int

const (
	screenAuth screen = iota
	screenInbox
	screenDetail
	screenSearch
)

type emailItem struct {
	id      string
	subject string
	from    string
	date    string
	snippet string
}

// Title returns the email subject for display in the list.
func (e emailItem) Title() string       { return e.subject }

// Description returns a formatted string with sender and date information.
func (e emailItem) Description() string { return e.from + "  |  " + e.date }

// FilterValue returns all searchable text fields concatenated for filtering in the list.
func (e emailItem) FilterValue() string { return e.subject + " " + e.from + " " + e.date }

type model struct {
	err error

	cfg   *oauth2.Config
	token *oauth2.Token
	store *store.TokenStore

	clientReady bool

	screen screen

	inbox list.Model

	detailVP viewport.Model
	detailID string

	searchInput textinput.Model
	query       string
	status      string

	width  int
	height int
}

var (
	box   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	pad   = lipgloss.NewStyle().Padding(1, 2)
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Faint(true)
)

// NewModel creates and initializes a new application model with default values.
// It sets up the inbox list, search input, detail viewport, and token store.
// Returns the model in the authentication screen state.
func NewModel() model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Inbox"
	l.SetShowHelp(true)

	si := textinput.New()
	si.Placeholder = "Gmail search query (example: from:someone newer_than:7d)"
	si.Prompt = "/ "
	si.Width = 60

	vp := viewport.New(0, 0)

	ts, _ := store.NewTokenStore()

	return model{
		screen:      screenAuth,
		inbox:       l,
		searchInput: si,
		detailVP:    vp,
		store:       ts,
		status:      "Press l to login in browser",
	}
}

// loadOAuthConfig reads the credentials.json file and creates an OAuth2 configuration
// for Gmail API access with read-only scope. Returns an error if the file is missing
// or cannot be parsed.
func loadOAuthConfig() (*oauth2.Config, error) {
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, errors.New("missing credentials.json in project root")
	}
	cfg, err := google.ConfigFromJSON(b, gmailReadonlyScope)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
