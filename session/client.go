package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client speaks a scope's web API at Base ("http://127.0.0.1:8080").
type Client struct {
	Base string
	HTTP *http.Client
	// Plan, when set, makes the client print each request instead of
	// sending it: the bench target's dry run.
	Plan io.Writer
}

// ErrPlanned is a request a dry run printed instead of sending.
var ErrPlanned = errors.New("session: dry run: request not sent")

// NewClient is a client of base with a request timeout.
// TRLC-LINKS: REQ-DSO-295
func NewClient(base string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Do sends one request with a JSON body (nil: none) and returns the reply's
// body and headers; a status other than 200 is an error.
// TRLC-LINKS: REQ-DSO-295
func (c *Client) Do(ctx context.Context, method, path string, body any) ([]byte, http.Header, error) {
	var rd io.Reader
	var text string
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		rd, text = bytes.NewReader(b), " "+string(b)
	}
	if c.Plan != nil {
		_, _ = fmt.Fprintf(c.Plan, "    %s %s%s%s\n", method, c.Base, path, text)
		return nil, nil, ErrPlanned
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return b, resp.Header, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(b[:min(len(b), 200)])))
	}
	return b, resp.Header, nil
}

// JSON sends a request and decodes its JSON reply into v (nil: ignored).
// TRLC-LINKS: REQ-DSO-295
func (c *Client) JSON(ctx context.Context, method, path string, body, v any) error {
	b, _, err := c.Do(ctx, method, path, body)
	if err != nil || v == nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	return nil
}

// okReply is the {"ok":…} shape most actions answer with; its error is
// under "err" or "error".
type okReply struct {
	OK    bool   `json:"ok"`
	Err   string `json:"err"`
	Error string `json:"error"`
}

// Post sends an action and fails unless the reply says ok.
// TRLC-LINKS: REQ-DSO-295
func (c *Client) Post(ctx context.Context, path string, body any) error {
	var r okReply
	if err := c.JSON(ctx, http.MethodPost, path, body, &r); err != nil {
		return err
	}
	if !r.OK {
		msg := r.Err + r.Error
		if msg == "" {
			msg = "refused"
		}
		return fmt.Errorf("POST %s: %s", path, msg)
	}
	return nil
}
