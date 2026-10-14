package openrouter

import "time"

// Budget is one spend guardrail reported by the key endpoint. A key can be
// subject to several at once (workspace, key, member), which is why the API
// returns a list plus the one that currently binds.
type Budget struct {
	Scope         string     `json:"scope"`
	GuardrailID   *string    `json:"guardrail_id"`
	BudgetID      *string    `json:"budget_id"`
	LimitUSD      float64    `json:"limit_usd"`
	SpendUSD      float64    `json:"spend_usd"`
	RemainingUSD  float64    `json:"remaining_usd"`
	ResetInterval string     `json:"reset_interval"`
	ResetsAt      *time.Time `json:"resets_at"`
	IncludeBYOK   bool       `json:"include_byok"`
}

// FreeModelDailyRequests is the daily request quota for the free model pool.
type FreeModelDailyRequests struct {
	Used      int `json:"used"`
	Limit     int `json:"limit"`
	Remaining int `json:"remaining"`
}

// DeprecatedRateLimit mirrors the key endpoint's rate_limit object. OpenRouter
// marks it deprecated; it is displayed for completeness only.
type DeprecatedRateLimit struct {
	Requests int    `json:"requests"`
	Interval string `json:"interval"`
	Note     string `json:"note"`
}

// KeyInfo is the usage and limit state of the authenticated key.
type KeyInfo struct {
	Label              string     `json:"label"`
	IsManagementKey    bool       `json:"is_management_key"`
	IsProvisioningKey  bool       `json:"is_provisioning_key"`
	Limit              *float64   `json:"limit"`
	LimitReset         string     `json:"limit_reset"`
	LimitRemaining     *float64   `json:"limit_remaining"`
	IncludeBYOKInLimit bool       `json:"include_byok_in_limit"`
	Budgets            []Budget   `json:"budgets"`
	EffectiveBudget    *Budget    `json:"effective_budget"`
	Usage              float64    `json:"usage"`
	UsageDaily         float64    `json:"usage_daily"`
	UsageWeekly        float64    `json:"usage_weekly"`
	UsageMonthly       float64    `json:"usage_monthly"`
	BYOKUsage          float64    `json:"byok_usage"`
	BYOKUsageDaily     float64    `json:"byok_usage_daily"`
	BYOKUsageWeekly    float64    `json:"byok_usage_weekly"`
	BYOKUsageMonthly   float64    `json:"byok_usage_monthly"`
	IsFreeTier         bool       `json:"is_free_tier"`
	ExpiresAt          *time.Time `json:"expires_at"`
	CreatorUserID      string     `json:"creator_user_id"`
	OrganizationID     *string    `json:"organization_id"`
	WorkspaceID        string     `json:"workspace_id"`
	AllowedDataRegions []string   `json:"allowed_data_regions"`

	FreeModelDailyRequests *FreeModelDailyRequests `json:"free_model_daily_requests"`
	RateLimit              *DeprecatedRateLimit    `json:"rate_limit"`
}

// Spend is the amount counted against the budget that currently binds,
// falling back to the key's own limit when the API reports no budget.
func (k KeyInfo) Spend() float64 {
	if k.EffectiveBudget != nil {
		return k.EffectiveBudget.SpendUSD
	}
	if k.Limit != nil {
		return max(0, *k.Limit-k.remainingOrZero())
	}
	return k.Usage
}

// LimitUSD is the ceiling of the budget that currently binds, or nil when the
// key is unlimited.
func (k KeyInfo) LimitUSD() *float64 {
	if k.EffectiveBudget != nil && k.EffectiveBudget.LimitUSD > 0 {
		limit := k.EffectiveBudget.LimitUSD
		return &limit
	}
	if k.Limit != nil && *k.Limit > 0 {
		return k.Limit
	}
	return nil
}

// ResetsAt is when the binding budget's spend returns to zero, or nil when
// the API reports no reset.
func (k KeyInfo) ResetsAt() *time.Time {
	if k.EffectiveBudget != nil && k.EffectiveBudget.ResetsAt != nil {
		return k.EffectiveBudget.ResetsAt
	}
	return nil
}

func (k KeyInfo) remainingOrZero() float64 {
	if k.LimitRemaining == nil {
		return 0
	}
	return *k.LimitRemaining
}

// Probe is the outcome of a single minimal chat completion, used to confirm
// the key can actually spend and to measure round-trip latency.
type Probe struct {
	Model            string
	Provider         string
	GenerationID     string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
	Text             string
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Choices  []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		Cost             float64 `json:"cost"`
	} `json:"usage"`
}

type keyEnvelope struct {
	Data KeyInfo `json:"data"`
}

type errorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}
