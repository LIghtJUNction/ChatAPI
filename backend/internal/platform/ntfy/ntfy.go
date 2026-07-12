package ntfy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"mime"
)

var ErrRedirectBlocked = errors.New("ntfy redirect blocked")
var ErrUnexpectedStatus = errors.New("ntfy unexpected status")

type Client struct {
	httpClient *http.Client
}

type Message struct {
	URL   string
	Title string
	Text  string
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Send(ctx context.Context, message Message) error {
	if c == nil {
		return nil
	}
	url := strings.TrimSpace(message.URL)
	text := strings.TrimSpace(message.Text)
	if url == "" || text == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(text))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if title := encodeTitle(strings.TrimSpace(message.Title)); title != "" {
		req.Header.Set("Title", title)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 1)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return ErrRedirectBlocked
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrUnexpectedStatus
	}
	return nil
}

func encodeTitle(value string) string {
	if value == "" {
		return ""
	}
	if isLatin1(value) {
		return value
	}
	return mime.BEncoding.Encode("utf-8", value)
}

func isLatin1(value string) bool {
	for _, ch := range value {
		if ch > 255 {
			return false
		}
	}
	return true
}
