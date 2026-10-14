// Package openrouter talks to the OpenRouter API: it reads a key's usage and
// budget state, optionally sends one minimal completion, and reports the
// transport-level observations (status, latency, rate-limit hints) a poller
// needs to pace itself.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the OpenRouter API root.
const DefaultBaseURL = "https://openrouter.ai/api/v1"

// DefaultTimeout bounds a single request.
const DefaultTimeout = 20 * time.Second

// DefaultProbeModel is the model used by the test request: a current
// low-cost flagship, so the probe costs a fraction of a cent.
const DefaultProbeModel = "openai/gpt-6-luna"

// Client performs authenticated OpenRouter requests.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client

	// Title is sent as X-Title so requests are attributable in the OpenRouter
	// activity dashboard. Empty omits the header.
	Title string
}

// New returns a client for the given key.
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: DefaultTimeout},
		Title:   "orusage",
	}
}

// Observation records what one request revealed about the connection, so the
// UI can show latency and rate-limit signals next to the data itself.
type Observation struct {
	FetchedAt  time.Time
	Latency    time.Duration
	StatusCode int
	Path       string
	Limits     RateLimits
}

// RateLimits holds rate-limit signals from the response headers. OpenRouter
// currently returns none of these, so Absent stays true and the UI says so
// rather than implying a limit it never saw.
type RateLimits struct {
	Limit         *int
	Remaining     *int
	Reset         *time.Time
	RetryAfter    time.Duration
	HasRetryAfter bool
	Absent        bool
}

// APIError is a non-2xx response.
type APIError struct {
	StatusCode    int
	Message       string
	RetryAfter    time.Duration
	HasRetryAfter bool
}

// Error implements error.
func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("openrouter: HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("openrouter: HTTP %d: %s", e.StatusCode, e.Message)
}

// RateLimited reports whether the response was a 429.
func (e *APIError) RateLimited() bool { return e.StatusCode == http.StatusTooManyRequests }

// Unauthorized reports whether the key was rejected.
func (e *APIError) Unauthorized() bool { return e.StatusCode == http.StatusUnauthorized }

// IsRateLimited reports whether err is a 429 API error.
func IsRateLimited(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.RateLimited()
}

// Key reads the usage and budget state of the configured key.
func (c *Client) Key(ctx context.Context) (KeyInfo, Observation, error) {
	var info keyEnvelope
	obs, err := c.do(ctx, http.MethodGet, "/key", nil, &info)
	if err != nil {
		return KeyInfo{}, obs, err
	}
	return info.Data, obs, nil
}

// Probe spends a negligible amount on one completion to verify the key can
// reach a provider, and reports the cost and latency of that request.
func (c *Client) Probe(ctx context.Context, model string) (Probe, Observation, error) {
	body := chatRequest{
		Model:     model,
		MaxTokens: 1,
		Messages:  []chatMessage{{Role: "user", Content: "ping"}},
	}
	var resp chatResponse
	obs, err := c.do(ctx, http.MethodPost, "/chat/completions", body, &resp)
	if err != nil {
		return Probe{}, obs, err
	}
	probe := Probe{
		Model:            resp.Model,
		Provider:         resp.Provider,
		GenerationID:     resp.ID,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
		CostUSD:          resp.Usage.Cost,
	}
	if len(resp.Choices) > 0 {
		probe.Text = strings.TrimSpace(resp.Choices[0].Message.Content)
	}
	return probe, obs, nil
}

func (c *Client) do(ctx context.Context, method, path string, payload any, out any) (Observation, error) {
	started := time.Now()
	obs := Observation{FetchedAt: started, Path: path}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return obs, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return obs, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "orusage")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Title != "" {
		req.Header.Set("X-Title", c.Title)
	}

	resp, err := c.HTTP.Do(req)
	obs.Latency = time.Since(started)
	if err != nil {
		return obs, err
	}
	defer func() { _ = resp.Body.Close() }()
	obs.StatusCode = resp.StatusCode
	obs.Limits = parseRateLimits(resp.Header)

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return obs, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return obs, newAPIError(resp.StatusCode, raw, obs.Limits)
	}
	if out == nil {
		return obs, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return obs, fmt.Errorf("decode response: %w", err)
	}
	return obs, nil
}

func newAPIError(status int, raw []byte, limits RateLimits) *APIError {
	err := &APIError{StatusCode: status, RetryAfter: limits.RetryAfter, HasRetryAfter: limits.HasRetryAfter}
	var envelope errorEnvelope
	if json.Unmarshal(raw, &envelope) == nil {
		err.Message = envelope.Error.Message
	}
	if err.Message == "" {
		err.Message = strings.TrimSpace(string(raw))
	}
	if len(err.Message) > 300 {
		err.Message = err.Message[:300] + "..."
	}
	return err
}

// parseRateLimits reads any rate-limit and retry hints present in a response,
// normalising the several encodings OpenRouter-compatible gateways use.
func parseRateLimits(h http.Header) RateLimits {
	out := RateLimits{}
	seen := false
	for _, name := range []string{"X-RateLimit-Limit", "X-RateLimit-Limit-Requests"} {
		if v := h.Get(name); v != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				out.Limit = &n
				seen = true
			}
		}
	}
	for _, name := range []string{"X-RateLimit-Remaining", "X-RateLimit-Remaining-Requests"} {
		if v := h.Get(name); v != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				out.Remaining = &n
				seen = true
			}
		}
	}
	for _, name := range []string{"X-RateLimit-Reset", "X-RateLimit-Reset-Requests"} {
		if v := h.Get(name); v != "" {
			if t, ok := parseReset(strings.TrimSpace(v)); ok {
				out.Reset = &t
				seen = true
			}
		}
	}
	if v := h.Get("Retry-After"); v != "" {
		if d, ok := parseRetryAfter(strings.TrimSpace(v)); ok {
			out.RetryAfter = d
			out.HasRetryAfter = true
			seen = true
		}
	}
	out.Absent = !seen
	return out
}

func parseReset(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		// A value this large is an absolute unix timestamp; a small one is a
		// countdown in seconds.
		if secs > 1e9 {
			return time.Unix(int64(secs), 0), true
		}
		return time.Now().Add(time.Duration(secs * float64(time.Second))), true
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	if d, err := time.ParseDuration(v); err == nil {
		return time.Now().Add(d), true
	}
	return time.Time{}, false
}

func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs * float64(time.Second)), true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}
