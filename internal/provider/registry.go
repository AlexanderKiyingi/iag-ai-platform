package provider

// Settings selects and configures the inference provider.
type Settings struct {
	AnthropicAPIKey  string
	AnthropicBaseURL string
	AnthropicVersion string
	DefaultModel     string
	MaxTokens        int
}

// Build returns the Anthropic (Claude) provider when an API key is configured,
// otherwise the deterministic stub. This lets local dev and tests run without
// any external credentials while production uses real inference.
func Build(s Settings) Provider {
	if s.AnthropicAPIKey != "" {
		return NewAnthropic(AnthropicConfig{
			APIKey:       s.AnthropicAPIKey,
			BaseURL:      s.AnthropicBaseURL,
			Version:      s.AnthropicVersion,
			DefaultModel: s.DefaultModel,
			MaxTokens:    s.MaxTokens,
		})
	}
	return NewStub(s.DefaultModel)
}
