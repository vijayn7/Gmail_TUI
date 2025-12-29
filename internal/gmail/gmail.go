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

func headerVal(headers []*gmail.MessagePartHeader, name string) string {
	ln := strings.ToLower(name)
	for _, h := range headers {
		if strings.ToLower(h.Name) == ln {
			return h.Value
		}
	}
	return ""
}

func (c *Client) ListInbox(ctx context.Context, max int64, query string) ([]EmailRow, error) {
	call := c.svc.Users.Messages.List("me").MaxResults(max)
	call = call.LabelIds("INBOX")
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

func HumanTimeoutCtx(parent context.Context, seconds int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, time.Duration(seconds)*time.Second)
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.svc.Users.GetProfile("me").Do()
	if err != nil {
		return fmt.Errorf("gmail ping failed: %w", err)
	}
	return nil
}
