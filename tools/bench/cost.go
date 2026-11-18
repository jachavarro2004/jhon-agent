package main

import "strings"

// Cost per provider (T146 FB3). A run's tokens are weighted by its
// provider's prices relative to an input token, so a GLM run is not costed
// at Claude's prices. These weights compare a Jhon cell with its no-Jhon
// counterpart on the same model; the subscription's quota points are a
// different measure, shown apart (quota_points, quota_weighted) and never
// mixed into this cost. cost_usd, when the stream gives one, is compared as
// it is.

// TokenWeights are one provider's prices per token type, relative to an
// input token.
type TokenWeights struct {
	Provider   string
	Input      float64
	CacheWrite float64
	CacheRead  float64
	Output     float64
}

// providerTokenWeights are every provider's weights, in one table.
//
// Sources:
//   - claude: Anthropic's API prices for every current Claude model are
//     input 1×, cache write (5 minutes) 1.25×, cache read 0.1×, output 5×
//     (e.g. Sonnet $3 / $3.75 / $0.30 / $15 per million tokens); the same
//     weights as the quota estimate (L432).
//   - glm (a GLM model on Z.ai or any provider but NaN): Z.ai's list prices
//     for GLM-5.3 per million tokens, input $1.40, cached input $0.26,
//     output $4.40 (docs.z.ai/guides/overview/pricing, read 2026-11-17).
//   - glm-nan (a GLM model on NaN): NaN's credits for glm5.3 per million
//     tokens, input 49, cached input 1.05, output 154 (nan.builders
//     model guide, November 2026; cloud.nan.builders/tokens is current).
//   - deepseek-nan (a DeepSeek model on NaN): NaN's credits for
//     deepseek-v4-flash per million tokens, input 10.5, cached input 0.21,
//     output 42 (same guide).
//     None charges a cache write apart from input, so a cache write
//     weighs as an input token.
var providerTokenWeights = []TokenWeights{
	{Provider: "claude", Input: 1, CacheWrite: 1.25, CacheRead: 0.1, Output: 5},
	{Provider: "glm", Input: 1, CacheWrite: 1, CacheRead: 0.26 / 1.40, Output: 4.40 / 1.40},
	{Provider: "glm-nan", Input: 1, CacheWrite: 1, CacheRead: 1.05 / 49, Output: 154.0 / 49},
	{Provider: "deepseek-nan", Input: 1, CacheWrite: 1, CacheRead: 0.21 / 10.5, Output: 42 / 10.5},
}

// tokenWeightsFor are the weights of model's provider and whether the table
// has them: a GLM model on NaN is glm-nan, any other GLM model (by its id)
// or Z.ai's provider is glm; Claude Code's models, claude-bridge and every other provider take
// Claude's weights, the others with known false.
func tokenWeightsFor(model string) (TokenWeights, bool) {
	provider, id, _ := strings.Cut(strings.ToLower(model), "/")
	if id == "" {
		provider, id = "", provider
	}
	byName := func(name string) TokenWeights {
		for _, w := range providerTokenWeights {
			if w.Provider == name {
				return w
			}
		}
		panic("no token weights for " + name)
	}
	switch {
	case strings.HasPrefix(id, "glm") && provider == "nan":
		return byName("glm-nan"), true
	case strings.HasPrefix(id, "deepseek") && provider == "nan":
		return byName("deepseek-nan"), true
	case strings.HasPrefix(id, "glm") || provider == "zai":
		return byName("glm"), true
	case provider == "" || provider == BridgeProvider || provider == "anthropic" || strings.HasPrefix(id, "claude"):
		return byName("claude"), true
	}
	return byName("claude"), false
}

// weigh is tokens weighted by w.
func (w TokenWeights) weigh(input, cacheWrite, cacheRead, output float64) float64 {
	return w.Input*input + w.CacheWrite*cacheWrite + w.CacheRead*cacheRead + w.Output*output
}

// costWeighted are a run's tokens weighted by its provider's prices.
func costWeighted(m Metrics) float64 {
	w, _ := tokenWeightsFor(m.Model)
	return w.weigh(m.InputTokens, m.CacheWriteTokens, m.CacheReadTokens, m.OutputTokens)
}
