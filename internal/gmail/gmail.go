package gmailx

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type Client struct {
	svc *gmail.Service
}

// New creates a new Gmail API client using the provided OAuth2 configuration and token.
// The client is configured with automatic token refresh and ready to make Gmail API calls.
// Returns an error if the Gmail service cannot be initialized.
func New(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (*Client, error) {
	httpClient := oauth2.NewClient(ctx, cfg.TokenSource(ctx, tok))
	svc, err := gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc}, nil
}

type EmailRow struct {
	ID      string
	Subject string
	From    string
	Date    string
	Snippet string
}

type EmailDetail struct {
	ID      string
	Subject string
	From    string
	To      string
	Date    string
	Snippet string
	Body    string
}

// headerVal extracts the value of a specific email header by name (case-insensitive).
// Searches through the Gmail message headers and returns the first matching value,
// or an empty string if the header is not found.
func headerVal(headers []*gmail.MessagePartHeader, name string) string {
	ln := strings.ToLower(name)
	for _, h := range headers {
		if strings.ToLower(h.Name) == ln {
			return h.Value
		}
	}
	return ""
}

// ListInbox fetches up to 'max' email messages from the user's Gmail inbox.
// If a query string is provided, it applies Gmail search syntax filtering
// (e.g., "from:someone newer_than:7d", "label:SENT"). Returns basic metadata including
// subject, sender, date, and snippet. Silently skips emails that fail to fetch.
// If the query contains a label filter, it won't apply the default INBOX filter.
func (c *Client) ListInbox(ctx context.Context, max int64, query string) ([]EmailRow, error) {
	call := c.svc.Users.Messages.List("me").MaxResults(max)

	// Only apply INBOX filter if query doesn't contain a label filter
	if !strings.Contains(strings.ToLower(query), "label:") {
		call = call.LabelIds("INBOX")
	}

	if strings.TrimSpace(query) != "" {
		call = call.Q(query)
	}

	ml, err := call.Do()
	if err != nil {
		return nil, err
	}

	out := make([]EmailRow, 0, len(ml.Messages))
	for _, m := range ml.Messages {
		msg, err := c.svc.Users.Messages.Get("me", m.Id).
			Format("metadata").
			MetadataHeaders("Subject", "From", "Date").
			Do()
		if err != nil {
			continue
		}

		subj := headerVal(msg.Payload.Headers, "Subject")
		if strings.TrimSpace(subj) == "" {
			subj = "(no subject)"
		}
		from := headerVal(msg.Payload.Headers, "From")
		date := headerVal(msg.Payload.Headers, "Date")

		out = append(out, EmailRow{
			ID:      m.Id,
			Subject: subj,
			From:    from,
			Date:    date,
			Snippet: msg.Snippet,
		})
	}
	return out, nil
}

// decodeB64URL decodes a URL-safe base64 encoded string to plain text.
// Gmail API uses base64url encoding for message bodies, which replaces
// '+' with '-' and '/' with '_', and omits padding. This function reverses
// those changes and properly decodes the content.
func decodeB64URL(s string) (string, error) {
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// extractBody recursively searches through email message parts to find and extract
// the plain text body. Gmail messages have a complex MIME structure with nested parts.
// This function prefers text/plain parts and decodes them from base64url encoding.
// Returns empty string if no plain text body is found.
func extractBody(part *gmail.MessagePart) string {
	if part == nil {
		return ""
	}

	mt := strings.ToLower(part.MimeType)
	if strings.HasPrefix(mt, "text/plain") && part.Body != nil && part.Body.Data != "" {
		txt, err := decodeB64URL(part.Body.Data)
		if err == nil {
			return txt
		}
		return ""
	}

	for _, p := range part.Parts {
		if b := extractBody(p); strings.TrimSpace(b) != "" {
			return b
		}
	}

	return ""
}

// GetDetail fetches the complete details of a specific email by ID.
// Returns full email content including all headers and the plain text body.
// The 'full' format includes the entire MIME structure of the message,
// allowing extraction of the message body and all metadata.
func (c *Client) GetDetail(ctx context.Context, id string) (*EmailDetail, error) {
	msg, err := c.svc.Users.Messages.Get("me", id).Format("full").Do()
	if err != nil {
		return nil, err
	}

	subj := headerVal(msg.Payload.Headers, "Subject")
	if strings.TrimSpace(subj) == "" {
		subj = "(no subject)"
	}

	body := extractBody(msg.Payload)
	if strings.TrimSpace(body) == "" {
		body = "(no plain-text body found)"
	}

	d := &EmailDetail{
		ID:      id,
		Subject: subj,
		From:    headerVal(msg.Payload.Headers, "From"),
		To:      headerVal(msg.Payload.Headers, "To"),
		Date:    headerVal(msg.Payload.Headers, "Date"),
		Snippet: msg.Snippet,
		Body:    body,
	}
	return d, nil
}

type Label struct {
	ID   string
	Name string
}

// ListLabels fetches all Gmail labels (both system and user-created) for the user's account.
// System labels include INBOX, SENT, DRAFT, TRASH, SPAM, etc. User labels are custom
// organizational tags. Returns a slice of labels with both ID and display Name.
func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	labelsResp, err := c.svc.Users.Labels.List("me").Do()
	if err != nil {
		return nil, err
	}
	labels := make([]Label, 0, len(labelsResp.Labels))
	for _, l := range labelsResp.Labels {
		labels = append(labels, Label{
			ID:   l.Id,
			Name: l.Name,
		})
	}
	return labels, nil
}

// HumanTimeoutCtx creates a context with a timeout specified in seconds.
// This is a convenience wrapper around context.WithTimeout that accepts
// seconds as an integer instead of a time.Duration, making it more readable.
func HumanTimeoutCtx(parent context.Context, seconds int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, time.Duration(seconds)*time.Second)
}

// Ping tests the Gmail API connection by fetching the user's profile.
// This is a lightweight check to verify that authentication is working
// and the Gmail API is accessible. Returns an error if the connection fails.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.svc.Users.GetProfile("me").Do()
	if err != nil {
		return fmt.Errorf("gmail ping failed: %w", err)
	}
	return nil
}
