// Package playit is a minimal client for the playit.gg control plane API.
//
// It deliberately has no dependency on terraform-plugin-framework so that it can
// be exercised on its own against a stub server.
//
// Three things about this API drive the design here:
//
//  1. Every call is a POST, including reads. There are no path or query
//     parameters; the whole input is the JSON body.
//  2. Failures are in-band. The service answers HTTP 200 with
//     {"status":"fail"} or {"status":"error"}; the HTTP status code is only
//     meaningful for 429. Branching on status code alone would read every
//     failure as a success.
//  3. Authorization uses a literal "Agent-Key" scheme, not "Bearer".
package playit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the public control plane endpoint.
	DefaultBaseURL = "https://api.playit.gg"

	defaultTimeout = 30 * time.Second
	maxAttempts    = 5
	maxBackoff     = 8 * time.Second
)

// Client talks to the playit control plane.
type Client struct {
	baseURL   string
	secretKey string
	httpc     *http.Client
}

// Option customises a Client.
type Option func(*Client)

// WithBaseURL overrides the API endpoint. Used by tests to target a stub server.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = strings.TrimRight(u, "/")
		}
	}
}

// WithHTTPClient supplies a custom *http.Client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpc = h
		}
	}
}

// NewClient builds a client authenticating with the given agent secret key.
func NewClient(secretKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		secretKey: secretKey,
		httpc:     &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// BaseURL reports the endpoint in use.
func (c *Client) BaseURL() string { return c.baseURL }

// envelope is the outer response shape: serde's adjacently tagged
// #[serde(tag = "status", content = "data")].
type envelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

// apiResponseError is the payload of {"status":"error"}.
type apiResponseError struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// call issues a POST to path and decodes the envelope into Res.
//
// This is a package-level function rather than a method because Go does not
// permit type parameters on methods.
func call[Res any](ctx context.Context, c *Client, path string, req any) (Res, error) {
	var zero Res

	payload, err := json.Marshal(req)
	if err != nil {
		return zero, &TransportError{Path: path, Err: fmt.Errorf("encoding request: %w", err)}
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepBackoff(ctx, attempt-1); err != nil {
				return zero, err
			}
		}

		body, retryable, err := c.roundTrip(ctx, path, payload)
		if err == nil {
			return decode[Res](path, body)
		}
		lastErr = err
		if !retryable {
			return zero, err
		}
	}
	return zero, lastErr
}

// roundTrip performs one HTTP exchange. It reports whether a failure is worth
// retrying; envelope-level failures are not its concern.
func (c *Client) roundTrip(ctx context.Context, path string, payload []byte) ([]byte, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, false, &TransportError{Path: path, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.secretKey != "" {
		httpReq.Header.Set("Authorization", "Agent-Key "+c.secretKey)
	}

	resp, err := c.httpc.Do(httpReq)
	if err != nil {
		// A cancelled context is the caller's decision, never a transient fault.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		return nil, true, &TransportError{Path: path, Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, &TransportError{Path: path, StatusCode: resp.StatusCode, Err: err}
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, true, &TransportError{Path: path, StatusCode: resp.StatusCode, Err: errors.New("rate limited")}
	case resp.StatusCode >= 500:
		return nil, true, &TransportError{Path: path, StatusCode: resp.StatusCode, Err: errors.New("server error")}
	case resp.StatusCode >= 400 && len(bytes.TrimSpace(body)) == 0:
		return nil, false, &TransportError{Path: path, StatusCode: resp.StatusCode, Err: errors.New("empty error response")}
	}

	// Any remaining 4xx still carries an envelope; let decode surface the typed
	// error rather than a bare status code.
	return body, false, nil
}

func decode[Res any](path string, body []byte) (Res, error) {
	var zero Res

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return zero, &TransportError{Path: path, Err: fmt.Errorf("decoding response envelope: %w", err)}
	}

	switch env.Status {
	case "success":
		// Endpoints returning () answer with a null payload.
		if len(env.Data) == 0 || string(env.Data) == "null" {
			return zero, nil
		}
		var out Res
		if err := json.Unmarshal(env.Data, &out); err != nil {
			return zero, &TransportError{Path: path, Err: fmt.Errorf("decoding response data: %w", err)}
		}
		return out, nil

	case "fail":
		var code string
		if err := json.Unmarshal(env.Data, &code); err != nil {
			// Keep an unexpected payload visible rather than swallowing it.
			code = string(env.Data)
		}
		return zero, &FailError{Path: path, Code: code}

	case "error":
		var apiErr apiResponseError
		if err := json.Unmarshal(env.Data, &apiErr); err != nil {
			return zero, &TransportError{Path: path, Err: fmt.Errorf("decoding api error: %w", err)}
		}
		return zero, &APIError{Path: path, Kind: apiErr.Type, Message: renderMessage(apiErr.Message)}

	default:
		return zero, &TransportError{Path: path, Err: fmt.Errorf("unexpected envelope status %q", env.Status)}
	}
}

// renderMessage flattens the polymorphic "message" field to something printable.
func renderMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func sleepBackoff(ctx context.Context, attempt int) error {
	d := time.Duration(float64(time.Second) * math.Pow(2, float64(attempt-1)))
	if d > maxBackoff {
		d = maxBackoff
	}
	d += time.Duration(rand.Int63n(int64(250 * time.Millisecond)))

	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
