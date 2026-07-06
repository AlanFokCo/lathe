// Package pricing turns raw token counts into a dollar estimate for the
// /cost slash and the statusline. Prices are per million tokens and reflect
// public list prices as of early 2026; use Update() to override.
package pricing

import "strings"

// Rate is the per-million-token dollar cost for one model.
type Rate struct {
	InPerMTok         float64
	OutPerMTok        float64
	CacheReadPerMTok  float64 // 0 disables cache_read pricing
	CacheWritePerMTok float64 // 0 disables cache_creation pricing
}

// Zero reports whether the rate carries no meaningful pricing info.
func (r Rate) Zero() bool { return r.InPerMTok == 0 && r.OutPerMTok == 0 }

// Estimate returns the dollar cost for the given token counts under rate.
// Cache tokens are billed at their own rates when non-zero, else fall back
// to the input rate.
func (r Rate) Estimate(in, out, cacheRead, cacheWrite int) float64 {
	inRate := r.InPerMTok
	crRate := r.CacheReadPerMTok
	if crRate == 0 {
		crRate = inRate
	}
	cwRate := r.CacheWritePerMTok
	if cwRate == 0 {
		cwRate = inRate
	}
	return (float64(in)*inRate +
		float64(out)*r.OutPerMTok +
		float64(cacheRead)*crRate +
		float64(cacheWrite)*cwRate) / 1_000_000.0
}

// table lists the built-in per-model rates. Keys are matched by
// case-insensitive prefix (so "claude-sonnet-4-6" matches "claude-sonnet-4").
// Ollama is $0 (local). Unknown models return (Rate{}, false).
var table = map[string]Rate{
	// Anthropic (2024/2025 list prices per Anthropic pricing page)
	"claude-opus-4":     {InPerMTok: 15.0, OutPerMTok: 75.0, CacheReadPerMTok: 1.50, CacheWritePerMTok: 18.75},
	"claude-sonnet-4":   {InPerMTok: 3.0, OutPerMTok: 15.0, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75},
	"claude-haiku-4":    {InPerMTok: 1.0, OutPerMTok: 5.0, CacheReadPerMTok: 0.10, CacheWritePerMTok: 1.25},
	"claude-3-5-sonnet": {InPerMTok: 3.0, OutPerMTok: 15.0, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75},
	"claude-3-5-haiku":  {InPerMTok: 0.80, OutPerMTok: 4.0, CacheReadPerMTok: 0.08, CacheWritePerMTok: 1.00},
	"claude-fable-5":    {InPerMTok: 3.0, OutPerMTok: 15.0}, // guess; user can override

	// OpenAI (per platform.openai.com/docs/pricing)
	"gpt-4o":      {InPerMTok: 2.50, OutPerMTok: 10.0},
	"gpt-4o-mini": {InPerMTok: 0.15, OutPerMTok: 0.60},
	"o1":          {InPerMTok: 15.0, OutPerMTok: 60.0},
	"o1-mini":     {InPerMTok: 3.0, OutPerMTok: 12.0},

	// DashScope Qwen (rough public rates converted to USD)
	"qwen-plus":  {InPerMTok: 0.5, OutPerMTok: 1.5},
	"qwen-max":   {InPerMTok: 8.0, OutPerMTok: 24.0},
	"qwen-turbo": {InPerMTok: 0.3, OutPerMTok: 0.6},

	// Ollama (local, free)
	"ollama": {},
}

// Lookup returns the Rate for a provider/model pair. Match is
// case-insensitive; matches the longest prefix in the built-in table so
// specific variants (claude-opus-4-8) fall back to the family entry
// (claude-opus-4). Ollama provider always returns Rate{}, true.
func Lookup(provider, model string) (Rate, bool) {
	if strings.EqualFold(provider, "ollama") {
		return Rate{}, true
	}
	m := strings.ToLower(model)
	var (
		best    Rate
		bestLen int
		found   bool
	)
	for prefix, r := range table {
		if strings.HasPrefix(m, prefix) && len(prefix) > bestLen {
			best = r
			bestLen = len(prefix)
			found = true
		}
	}
	return best, found
}

// Update installs or overrides a rate. Callers can use this to teach lathe
// about a custom or newly-released model without recompiling.
func Update(modelPrefix string, r Rate) { table[strings.ToLower(modelPrefix)] = r }
