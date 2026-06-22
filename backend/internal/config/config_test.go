package config

import "testing"

// TestAIDefaults asserts the documented AI defaults are applied when no
// AI-specific environment variables are present.
func TestAIDefaults(t *testing.T) {
	// Ensure no leaking env values from the host influence the defaults.
	for _, k := range []string{
		"AI_CHAT_MODEL", "OPENAI_BASE_URL", "OPENAI_API_KEY",
		"AI_IMAGE_PROVIDER", "AI_IMAGE_MODEL", "AI_IMAGE_API_KEY", "FAL_KEY",
		"AI_SEARCH_PROVIDER", "AI_SEARCH_API_KEY",
	} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.AIChatModel != defaultAIChatModel {
		t.Errorf("AIChatModel = %q, want %q", cfg.AIChatModel, defaultAIChatModel)
	}
	if cfg.OpenAIBaseURL != defaultOpenAIBaseURL {
		t.Errorf("OpenAIBaseURL = %q, want %q", cfg.OpenAIBaseURL, defaultOpenAIBaseURL)
	}
	if cfg.AIImageProvider != defaultAIImageProvider {
		t.Errorf("AIImageProvider = %q, want %q", cfg.AIImageProvider, defaultAIImageProvider)
	}
	if cfg.AISearchProvider != defaultAISearchProvider {
		t.Errorf("AISearchProvider = %q, want %q", cfg.AISearchProvider, defaultAISearchProvider)
	}
}

// TestSearchDefaultsToBraveWhenKeySet asserts that supplying only the search
// key still applies the default `brave` provider (runtime-spec scenario).
func TestSearchDefaultsToBraveWhenKeySet(t *testing.T) {
	t.Setenv("AI_SEARCH_PROVIDER", "")
	t.Setenv("AI_SEARCH_API_KEY", "secret-search-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AISearchProvider != "brave" {
		t.Errorf("AISearchProvider = %q, want brave", cfg.AISearchProvider)
	}
	if !cfg.WebSearchEnabled() {
		t.Errorf("WebSearchEnabled() = false, want true when search key set")
	}
}

// TestWebSearchDisabledWhenNoKey asserts the search tool is reported disabled
// when no credential is configured.
func TestWebSearchDisabledWhenNoKey(t *testing.T) {
	t.Setenv("AI_SEARCH_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WebSearchEnabled() {
		t.Errorf("WebSearchEnabled() = true, want false when no search key")
	}
}

// TestEnvOverridesAIDefaults asserts environment values take precedence over
// the built-in defaults.
func TestEnvOverridesAIDefaults(t *testing.T) {
	t.Setenv("AI_CHAT_MODEL", "custom-model")
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("AI_IMAGE_PROVIDER", "gemini")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AIChatModel != "custom-model" {
		t.Errorf("AIChatModel = %q, want custom-model", cfg.AIChatModel)
	}
	if cfg.OpenAIBaseURL != "https://example.test/v1" {
		t.Errorf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
	}
	if cfg.AIImageProvider != "gemini" {
		t.Errorf("AIImageProvider = %q, want gemini", cfg.AIImageProvider)
	}
}

// TestFalKeyFallback asserts FAL_KEY is used when AI_IMAGE_API_KEY is unset and
// that AI_IMAGE_API_KEY takes precedence when both are present.
func TestFalKeyFallback(t *testing.T) {
	t.Run("fallback", func(t *testing.T) {
		t.Setenv("AI_IMAGE_API_KEY", "")
		t.Setenv("FAL_KEY", "fal-secret")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.AIImageAPIKey != "fal-secret" {
			t.Errorf("AIImageAPIKey = %q, want fal-secret", cfg.AIImageAPIKey)
		}
	})

	t.Run("precedence", func(t *testing.T) {
		t.Setenv("AI_IMAGE_API_KEY", "primary")
		t.Setenv("FAL_KEY", "fallback")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.AIImageAPIKey != "primary" {
			t.Errorf("AIImageAPIKey = %q, want primary", cfg.AIImageAPIKey)
		}
	})
}
