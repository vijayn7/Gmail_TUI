package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

const (
	credentialsFile = "credentials.json"
	// Read-only scope for MVP. :contentReference[oaicite:4]{index=4}
	gmailScope = "https://www.googleapis.com/auth/gmail.readonly"
)

type installedCreds struct {
	Installed struct {
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthURI      string   `json:"auth_uri"`
		TokenURI     string   `json:"token_uri"`
	} `json:"installed"`
}

func appDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".gmail-tui")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func tokenPath() (string, error) {
	dir, err := appDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token.json"), nil
}

func loadToken() (*oauth2.Token, error) {
	p, err := tokenPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func saveToken(t *oauth2.Token) error {
	p, err := tokenPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0600)
}

func readCredentials() (installedCreds, error) {
	var c installedCreds
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return c, fmt.Errorf("missing %s: %w", credentialsFile, err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Installed.ClientID == "" || c.Installed.TokenURI == "" {
		return c, errors.New("credentials.json missing installed client_id or token_uri")
	}
	return c, nil
}

// Device Authorization Flow (modern replacement for OOB “paste code”). :contentReference[oaicite:5]{index=5}
type deviceCodeResp struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func startDeviceFlow(ctx context.Context, clientID string, scopes []string) (deviceCodeResp, error) {
	var out deviceCodeResp
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", strings.Join(scopes, " "))

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/device/code", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return out, fmt.Errorf("device code failed: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	if out.Interval == 0 {
		out.Interval = 5
	}
	return out, nil
}

type tokenErr struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func pollDeviceToken(ctx context.Context, tokenURI, clientID, clientSecret, deviceCode string, intervalSec int, expiresInSec int) (*oauth2.Token, error) {
	deadline := time.Now().Add(time.Duration(expiresInSec) * time.Second)

	for {
		if time.Now().After(deadline) {
			return nil, errors.New("device login expired, try again")
		}

		form := url.Values{}
		form.Set("client_id", clientID)
		if clientSecret != "" {
			form.Set("client_secret", clientSecret)
		}
		form.Set("device_code", deviceCode)
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

		req, _ := http.NewRequestWithContext(ctx, "POST", tokenURI, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 200 {
			var tok oauth2.Token
			if err := json.Unmarshal(b, &tok); err != nil {
				return nil, err
			}
			if tok.AccessToken == "" {
				return nil, errors.New("token response missing access_token")
			}
			return &tok, nil
		}

		var te tokenErr
		_ = json.Unmarshal(b, &te)

		switch te.Error {
		case "authorization_pending":
			time.Sleep(time.Duration(intervalSec) * time.Second)
			continue
		case "slow_down":
			intervalSec += 2
			time.Sleep(time.Duration(intervalSec) * time.Second)
			continue
		case "access_denied":
			return nil, errors.New("access denied in browser")
		default:
			if te.Error != "" {
				return nil, fmt.Errorf("token error: %s (%s)", te.Error, te.ErrorDescription)
			}
			return nil, fmt.Errorf("token request failed: %s %s", resp.Status, strings.TrimSpace(string(b)))
		}
	}
}

func oauthConfig(creds installedCreds) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     creds.Installed.ClientID,
		ClientSecret: creds.Installed.ClientSecret,
		Scopes:       []string{gmailScope},
		Endpoint: oauth2.Endpoint{
			AuthURL:  creds.Installed.AuthURI,
			TokenURL: creds.Installed.TokenURI,
		},
	}
}

type emailItem struct {
	id      string
	subject string
	from    string
}

func (e emailItem) Title() string       { return e.subject }
func (e emailItem) Description() string { return e.from }
func (e emailItem) FilterValue() string { return e.subject + " " + e.from }

type authStep int

const (
	authIdle authStep = iota
	authNeedLogin
	authWaiting
	authDone
)

type authInfoMsg struct {
	url     string
	userCode string
}

type inboxMsg struct {
	items []list.Item
	err   error
}

type model struct {
	step authStep
	err  error

	verificationURL string
	userCode        string

	lst list.Model

	cfg   *oauth2.Config
	token *oauth2.Token
}

func newModel() model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Gmail Inbox"
	l.SetShowHelp(true)

	return model{
		step: authIdle,
		lst:  l,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadCredsCmd(), loadTokenCmd())
}

type credsMsg struct {
	cfg   *oauth2.Config
	tokenURI string
	clientID string
	clientSecret string
	err   error
}

func loadCredsCmd() tea.Cmd {
	return func() tea.Msg {
		creds, err := readCredentials()
		if err != nil {
			return credsMsg{err: err}
		}
		return credsMsg{
			cfg: oauthConfig(creds),
			tokenURI: creds.Installed.TokenURI,
			clientID: creds.Installed.ClientID,
			clientSecret: creds.Installed.ClientSecret,
		}
	}
}

type tokenMsg struct {
	tok *oauth2.Token
	err error
}

func loadTokenCmd() tea.Cmd {
	return func() tea.Msg {
		tok, err := loadToken()
		if err != nil {
			return tokenMsg{tok: nil, err: err}
		}
		return tokenMsg{tok: tok, err: nil}
	}
}

type beginDeviceFlowMsg struct {
	tokenURI string
	clientID string
	clientSecret string
}

func beginDeviceFlowCmd(tokenURI, clientID, clientSecret string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		dc, err := startDeviceFlow(ctx, clientID, []string{gmailScope})
		if err != nil {
			return inboxMsg{err: err}
		}
		return authInfoMsg{url: dc.VerificationURI, userCode: dc.UserCode}
	}
}

func waitForDeviceTokenCmd(tokenURI, clientID, clientSecret, deviceCode string, intervalSec, expiresSec int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(expiresSec+30)*time.Second)
		defer cancel()

		tok, err := pollDeviceToken(ctx, tokenURI, clientID, clientSecret, deviceCode, intervalSec, expiresSec)
		if err != nil {
			return inboxMsg{err: err}
		}
		_ = saveToken(tok)
		return tokenMsg{tok: tok, err: nil}
	}
}

func fetchInboxCmd(cfg *oauth2.Config, tok *oauth2.Token) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		client := oauth2.NewClient(ctx, cfg.TokenSource(ctx, tok))
		svc, err := gmail.NewService(ctx, option.WithHTTPClient(client))
		if err != nil {
			return inboxMsg{err: err}
		}

		// List messages in INBOX
		msgList, err := svc.Users.Messages.List("me").LabelIds("INBOX").MaxResults(25).Do()
		if err != nil {
			return inboxMsg{err: err}
		}

		var items []list.Item
		for _, m := range msgList.Messages {
			get, err := svc.Users.Messages.Get("me", m.Id).Format("metadata").MetadataHeaders("Subject", "From").Do()
			if err != nil {
				continue
			}
			subj := "(no subject)"
			from := ""
			for _, h := range get.Payload.Headers {
				switch strings.ToLower(h.Name) {
				case "subject":
					if strings.TrimSpace(h.Value) != "" {
						subj = h.Value
					}
				case "from":
					from = h.Value
				}
			}
			items = append(items, emailItem{id: m.Id, subject: subj, from: from})
		}

		return inboxMsg{items: items, err: nil}
	}
}

var (
	box   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	pad   = lipgloss.NewStyle().Padding(1, 2)
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Faint(true)
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.lst.SetSize(msg.Width-4, msg.Height-6)
		return m, nil

	case credsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.cfg = msg.cfg
		return m, nil

	case tokenMsg:
		if msg.err == nil && msg.tok != nil {
			m.token = msg.tok
			m.step = authDone
			return m, fetchInboxCmd(m.cfg, m.token)
		}
		// If token missing, we will show login prompt.
		if m.token == nil {
			m.step = authNeedLogin
		}
		return m, nil

	case authInfoMsg:
		// Start a fresh device flow, then poll token endpoint.
		// We need device_code + interval/expires; simplest approach is to restart device flow and keep device_code too.
		// To keep this starter short, we do it via a second request right away:
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		creds, err := readCredentials()
		if err != nil {
			m.err = err
			return m, nil
		}
		dc, err := startDeviceFlow(ctx, creds.Installed.ClientID, []string{gmailScope})
		if err != nil {
			m.err = err
			return m, nil
		}
		m.verificationURL = dc.VerificationURI
		m.userCode = dc.UserCode
		m.step = authWaiting
		return m, waitForDeviceTokenCmd(creds.Installed.TokenURI, creds.Installed.ClientID, creds.Installed.ClientSecret, dc.DeviceCode, dc.Interval, dc.ExpiresIn)

	case inboxMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.lst.SetItems(msg.items)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "l":
			// Start device login.
			m.err = nil
			if m.cfg == nil {
				m.err = errors.New("missing credentials.json")
				return m, nil
			}
			m.step = authWaiting
			return m, func() tea.Msg {
				// Trigger authInfoMsg which starts device flow and polling
				return authInfoMsg{}
			}
		case "r":
			if m.cfg != nil && m.token != nil {
				return m, fetchInboxCmd(m.cfg, m.token)
			}
		}
		if m.step == authDone {
			var cmd tea.Cmd
			m.lst, cmd = m.lst.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m model) View() string {
	title := bold.Render("Gmail TUI")

	if m.err != nil {
		return pad.Render(box.Render(title+"\n\nError: "+m.err.Error()+"\n\n"+faint.Render("q quit"))) + "\n"
	}

	// No token yet
	if m.step != authDone {
		body := "Press l to login.\n\n"
		if m.step == authWaiting && m.verificationURL != "" && m.userCode != "" {
			body += "In your browser, go to:\n" + m.verificationURL + "\n\nEnter code:\n" + m.userCode + "\n\nWaiting for authorization...\n"
		} else {
			body += faint.Render("This uses Device Authorization flow (no redirect).") + "\n"
		}
		body += "\n" + faint.Render("l login • q quit")
		return pad.Render(box.Render(title+"\n\n"+body)) + "\n"
	}

	// Inbox
	header := title + "\n" + faint.Render("r refresh • q quit")
	return pad.Render(box.Render(header+"\n\n"+m.lst.View())) + "\n"
}

func main() {
	// Quick sanity check that credentials.json exists early.
	if _, err := os.Stat(credentialsFile); err != nil {
		fmt.Println("Missing credentials.json. Download it from Google Cloud Console (Desktop OAuth client).")
		os.Exit(1)
	}

	p := tea.NewProgram(newModel())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
