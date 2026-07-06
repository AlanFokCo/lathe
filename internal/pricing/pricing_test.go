package pricing

import (
	"math"
	"testing"
)

func TestLookupPrefixMatch(t *testing.T) {
	r, ok := Lookup("anthropic", "claude-sonnet-4-6")
	if !ok || r.InPerMTok != 3.0 || r.OutPerMTok != 15.0 {
		t.Fatalf("sonnet-4-6 lookup: ok=%v rate=%+v", ok, r)
	}
	if _, ok := Lookup("anthropic", "unknown-model-xyz"); ok {
		t.Fatal("unknown model should return ok=false")
	}
}

func TestLookupOllamaIsFree(t *testing.T) {
	r, ok := Lookup("ollama", "qwen2.5-coder")
	if !ok || !r.Zero() {
		t.Fatalf("ollama should be (Rate{}, true), got (%+v, %v)", r, ok)
	}
}

func TestEstimateComputesTokens(t *testing.T) {
	r := Rate{InPerMTok: 3.0, OutPerMTok: 15.0}
	got := r.Estimate(1_000_000, 500_000, 0, 0)
	// 1M input at $3 + 0.5M output at $15 = $3 + $7.5 = $10.5
	want := 10.5
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("estimate = %f, want %f", got, want)
	}
}

func TestEstimateUsesCacheRates(t *testing.T) {
	r := Rate{InPerMTok: 3.0, OutPerMTok: 15.0, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75}
	got := r.Estimate(0, 0, 1_000_000, 1_000_000)
	// 1M cache read at $0.30 + 1M cache write at $3.75 = $4.05
	want := 4.05
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("cache estimate = %f, want %f", got, want)
	}
}

// TestEstimateCacheFallbackToInputRate — a model with no explicit cache rates
// bills cache tokens at the input rate (better than losing the tokens
// entirely).
func TestEstimateCacheFallbackToInputRate(t *testing.T) {
	r := Rate{InPerMTok: 3.0, OutPerMTok: 15.0}
	got := r.Estimate(0, 0, 1_000_000, 0)
	if math.Abs(got-3.0) > 1e-9 {
		t.Fatalf("cache-read fallback = %f, want 3", got)
	}
}

func TestUpdateOverride(t *testing.T) {
	Update("my-custom", Rate{InPerMTok: 42, OutPerMTok: 99})
	r, ok := Lookup("openai", "my-custom-v1")
	if !ok || r.InPerMTok != 42 {
		t.Fatalf("Update did not take effect: (%+v, %v)", r, ok)
	}
}
