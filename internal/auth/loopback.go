package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// openBrowser opens the specified URL in the user's default web browser.
// Uses platform-specific commands: 'open' on macOS, 'rundll32' on Windows,
// and 'xdg-open' on Linux/Unix systems.
func openBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

// randState generates a cryptographically secure random state parameter
// for OAuth2 authentication. This prevents CSRF attacks by ensuring the
// authorization response matches the original request. Returns a base64-encoded string.
func randState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// LoopbackLogin implements the OAuth2 authorization code flow using a local loopback server.
// It starts a temporary HTTP server on 127.0.0.1 with a random available port,
// opens the user's browser to Google's authorization page, waits for the callback
// with the authorization code, then exchanges the code for access and refresh tokens.
// Times out after 2 minutes if the user doesn't complete authorization.
func LoopbackLogin(cfg *oauth2.Config) (*oauth2.Token, error) {
	state, err := randState()
	if err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	cfgCopy := *cfg
	cfgCopy.RedirectURL = redirect

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- errors.New("oauth state mismatch")
			return
		}
		if e := q.Get("error"); e != "" {
			http.Error(w, e, http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth error: %s", e)
			return
		}
		code := q.Get("code")
		if strings.TrimSpace(code) == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- errors.New("missing code")
			return
		}

		_, _ = fmt.Fprintln(w, "Authorized. You can close this tab and return to the app.")
		codeCh <- code
	})

	go func() { _ = srv.Serve(ln) }()

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	authURL := cfgCopy.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	if err := openBrowser(authURL); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var code string
	select {
	case code = <-codeCh:
	case e := <-errCh:
		return nil, e
	case <-ctx.Done():
		return nil, errors.New("login timed out")
	}

	tok, err := cfgCopy.Exchange(context.Background(), code)
	if err != nil {
		return nil, err
	}
	return tok, nil
}
