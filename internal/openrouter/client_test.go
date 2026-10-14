package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleKeyBody = `{
  "data": {
    "label": "sk-or-v1-8c1...8d8",
    "is_management_key": false,
    "is_provisioning_key": false,
    "limit": 250,
    "limit_reset": "monthly",
    "limit_remaining": 208.638643102,
    "include_byok_in_limit": false,
    "budgets": [
      {"scope":"key_limit","guardrail_id":null,"budget_id":null,"limit_usd":250,"spend_usd":41.361356898,
       "remaining_usd":208.638643102,"reset_interval":"monthly","resets_at":"2026-11-01T00:00:00.000Z","include_byok":false}
    ],
    "effective_budget": {"scope":"key_limit","guardrail_id":null,"budget_id":null,"limit_usd":250,
      "spend_usd":41.361356898,"remaining_usd":208.638643102,"reset_interval":"monthly",
      "resets_at":"2026-11-01T00:00:00.000Z","include_byok":false},
    "usage": 41.361356898,
    "usage_daily": 0,
    "usage_weekly": 33.966906289,
    "usage_monthly": 41.361356898,
    "byok_usage": 0,
    "byok_usage_daily": 0,
    "byok_usage_weekly": 0,
    "byok_usage_monthly": 0,
    "is_free_tier": false,
    "expires_at": "2027-10-08T08:16:20.143Z",
    "creator_user_id": "user_3IirijgeHak9NpQrORDgo13Blx0",
    "organization_id": "org_3KP1RLLOTiCPBDddMWQlvwdqREx",
    "workspace_id": "69a4d90a-5539-59c0-abd8-f8941c021e60",
    "allowed_data_regions": ["global"],
    "free_model_daily_requests": {"used": 0, "limit": 1000, "remaining": 1000},
    "rate_limit": {"requests": -1, "interval": "10s", "note": "This field is deprecated and safe to ignore."}
  }
}`

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(server.URL, "test-key"), server
}

func TestKeyParsesUsageAndBudgets(t *testing.T) {
	var gotAuth, gotTitle string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTitle = r.Header.Get("X-Title")
		if r.URL.Path != "/key" {
			t.Errorf("path = %q, want /key", r.URL.Path)
		}
		_, _ = w.Write([]byte(sampleKeyBody))
	})

	info, obs, err := client.Key(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotTitle != "orusage" {
		t.Errorf("X-Title = %q", gotTitle)
	}
	if info.Limit == nil || *info.Limit != 250 {
		t.Fatalf("limit = %v, want 250", info.Limit)
	}
	if len(info.Budgets) != 1 || info.Budgets[0].Scope != "key_limit" {
		t.Fatalf("budgets = %+v", info.Budgets)
	}
	if info.EffectiveBudget == nil || info.EffectiveBudget.ResetInterval != "monthly" {
		t.Fatalf("effective budget = %+v", info.EffectiveBudget)
	}
	if info.FreeModelDailyRequests == nil || info.FreeModelDailyRequests.Limit != 1000 {
		t.Fatalf("free model quota = %+v", info.FreeModelDailyRequests)
	}
	if got := info.Spend(); got != 41.361356898 {
		t.Errorf("Spend = %v, want the effective budget spend", got)
	}
	if limit := info.LimitUSD(); limit == nil || *limit != 250 {
		t.Errorf("LimitUSD = %v, want 250", limit)
	}
	if resets := info.ResetsAt(); resets == nil || !resets.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ResetsAt = %v", resets)
	}
	if obs.StatusCode != 200 {
		t.Errorf("status = %d, want 200", obs.StatusCode)
	}
	if !obs.Limits.Absent {
		t.Error("Limits.Absent = false for a response carrying no rate-limit headers")
	}
}

func TestKeyHandlesUnlimitedKey(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"limit":null,"limit_remaining":null,"usage":12.5,"budgets":[]}}`))
	})
	info, _, err := client.Key(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if limit := info.LimitUSD(); limit != nil {
		t.Errorf("LimitUSD = %v, want nil for an unlimited key", limit)
	}
	if got := info.Spend(); got != 12.5 {
		t.Errorf("Spend = %v, want the lifetime usage as a fallback", got)
	}
	if info.ResetsAt() != nil {
		t.Errorf("ResetsAt = %v, want nil", info.ResetsAt())
	}
}

func TestKeyErrors(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"No auth credentials found","code":401}}`))
		})
		_, _, err := client.Key(context.Background())
		var apiErr *APIError
		if !asAPIError(err, &apiErr) {
			t.Fatalf("err = %v, want *APIError", err)
		}
		if !apiErr.Unauthorized() || apiErr.RateLimited() {
			t.Errorf("classification wrong: %+v", apiErr)
		}
		if !strings.Contains(apiErr.Error(), "No auth credentials found") {
			t.Errorf("message lost: %v", apiErr)
		}
	})

	t.Run("rate limited honors retry-after", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "7")
			w.Header().Set("X-RateLimit-Limit", "20")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
		})
		_, obs, err := client.Key(context.Background())
		if !IsRateLimited(err) {
			t.Fatalf("err = %v, want a rate-limit error", err)
		}
		if obs.Limits.Absent {
			t.Error("Limits.Absent = true despite rate-limit headers")
		}
		if !obs.Limits.HasRetryAfter || obs.Limits.RetryAfter != 7*time.Second {
			t.Errorf("RetryAfter = %v (present=%v), want 7s", obs.Limits.RetryAfter, obs.Limits.HasRetryAfter)
		}
		if obs.Limits.Limit == nil || *obs.Limits.Limit != 20 {
			t.Errorf("Limit = %v, want 20", obs.Limits.Limit)
		}
		if obs.Limits.Remaining == nil || *obs.Limits.Remaining != 0 {
			t.Errorf("Remaining = %v, want 0", obs.Limits.Remaining)
		}
		var apiErr *APIError
		if !asAPIError(err, &apiErr) || !apiErr.HasRetryAfter || apiErr.RetryAfter != 7*time.Second {
			t.Errorf("APIError did not carry the retry hint: %+v", apiErr)
		}
	})

	t.Run("malformed body", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data": {`))
		})
		if _, _, err := client.Key(context.Background()); err == nil {
			t.Fatal("expected a decode error")
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		client := New("http://127.0.0.1:1", "k")
		client.HTTP.Timeout = 2 * time.Second
		if _, _, err := client.Key(context.Background()); err == nil {
			t.Fatal("expected a transport error")
		}
	})
}

func TestProbeSendsMinimalCompletion(t *testing.T) {
	var body chatRequest
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"gen-1","model":"openai/gpt-4o-mini","provider":"Azure",
          "choices":[{"message":{"role":"assistant","content":"Hello"}}],
          "usage":{"prompt_tokens":8,"completion_tokens":1,"total_tokens":9,"cost":0.0000018}}`))
	})

	probe, obs, err := client.Probe(context.Background(), "openai/gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	if body.MaxTokens != 1 || body.Model != "openai/gpt-4o-mini" || len(body.Messages) != 1 {
		t.Errorf("probe request = %+v, want a single-token single-message completion", body)
	}
	if probe.TotalTokens != 9 || probe.CostUSD != 0.0000018 || probe.Provider != "Azure" || probe.Text != "Hello" {
		t.Errorf("probe = %+v", probe)
	}
	if obs.Path != "/chat/completions" {
		t.Errorf("observation path = %q", obs.Path)
	}
}

func TestParseResetAndRetryAfterEncodings(t *testing.T) {
	if t1, ok := parseReset("1793500800"); !ok || t1.Year() != 2026 {
		t.Errorf("unix reset = %v (ok=%v)", t1, ok)
	}
	if t2, ok := parseReset("30"); !ok || time.Until(t2) < 25*time.Second {
		t.Errorf("countdown reset = %v (ok=%v)", t2, ok)
	}
	if t3, ok := parseReset("2026-11-01T00:00:00Z"); !ok || t3.Month() != time.November {
		t.Errorf("rfc3339 reset = %v (ok=%v)", t3, ok)
	}
	if _, ok := parseReset("soon"); ok {
		t.Error("parseReset accepted a non-time")
	}
	if d, ok := parseRetryAfter("2.5"); !ok || d != 2500*time.Millisecond {
		t.Errorf("retry-after seconds = %v (ok=%v)", d, ok)
	}
	if _, ok := parseRetryAfter("nonsense"); ok {
		t.Error("parseRetryAfter accepted a non-duration")
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	client := New(DefaultBaseURL+"/", "k")
	if client.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q", client.BaseURL)
	}
}

func asAPIError(err error, target **APIError) bool {
	apiErr, ok := err.(*APIError)
	if ok {
		*target = apiErr
	}
	return ok
}
