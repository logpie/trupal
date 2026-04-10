package main

import "testing"

func TestConfigValidateNormalizesBrainSettings(t *testing.T) {
	cfg := Config{
		PollInterval:    3,
		SessionProvider: " CODEX ",
		BrainProvider:   " CLAUDE ",
		BrainModel:      " SONNET ",
		BrainEffort:     " HIGH ",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	if cfg.SessionProvider != "codex" {
		t.Fatalf("expected normalized session provider, got %q", cfg.SessionProvider)
	}
	if cfg.BrainProvider != "claude" {
		t.Fatalf("expected normalized provider, got %q", cfg.BrainProvider)
	}
	if cfg.BrainModel != "sonnet" {
		t.Fatalf("expected normalized model, got %q", cfg.BrainModel)
	}
	if cfg.BrainEffort != "high" {
		t.Fatalf("expected normalized effort, got %q", cfg.BrainEffort)
	}
}

func TestConfigValidateRejectsInvalidBrainSettings(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "invalid model",
			cfg: Config{
				PollInterval:    3,
				SessionProvider: "claude",
				BrainProvider:   "claude",
				BrainModel:      "invalid",
				BrainEffort:     "high",
			},
		},
		{
			name: "invalid effort",
			cfg: Config{
				PollInterval:    3,
				SessionProvider: "claude",
				BrainProvider:   "claude",
				BrainModel:      "sonnet",
				BrainEffort:     "turbo",
			},
		},
		{
			name: "invalid session provider",
			cfg: Config{
				PollInterval:    3,
				SessionProvider: "invalid",
				BrainProvider:   "claude",
				BrainModel:      "sonnet",
				BrainEffort:     "high",
			},
		},
		{
			name: "claude provider with codex model",
			cfg: Config{
				PollInterval:    3,
				SessionProvider: "claude",
				BrainProvider:   "claude",
				BrainModel:      "gpt-5.4",
				BrainEffort:     "high",
			},
		},
		{
			name: "codex provider with claude model",
			cfg: Config{
				PollInterval:    3,
				SessionProvider: "codex",
				BrainProvider:   "codex",
				BrainModel:      "sonnet",
				BrainEffort:     "high",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err == nil {
				t.Fatal("expected Validate() to fail")
			}
		})
	}
}

func TestConfigValidateRejectsInvalidPollInterval(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "too low",
			cfg: Config{
				PollInterval:    0,
				SessionProvider: "claude",
				BrainProvider:   "claude",
				BrainModel:      "sonnet",
				BrainEffort:     "high",
			},
		},
		{
			name: "too high",
			cfg: Config{
				PollInterval:    61,
				SessionProvider: "claude",
				BrainProvider:   "claude",
				BrainModel:      "sonnet",
				BrainEffort:     "high",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err == nil {
				t.Fatal("expected Validate() to fail")
			}
		})
	}
}

func TestConfigValidateAllowsCodexDefaults(t *testing.T) {
	cfg := Config{
		PollInterval:    3,
		SessionProvider: "codex",
		BrainProvider:   "codex",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	if cfg.BrainModel != "" {
		t.Fatalf("expected codex default model to remain empty, got %q", cfg.BrainModel)
	}
	if cfg.BrainEffort != "high" {
		t.Fatalf("expected default effort high, got %q", cfg.BrainEffort)
	}
}

func TestParseTomlLineStripsInlineComments(t *testing.T) {
	key, value, ok := parseTomlLine(`brain_provider = "codex"  # inline comment`)
	if !ok {
		t.Fatal("expected parseTomlLine to succeed")
	}
	if key != "brain_provider" || value != "codex" {
		t.Fatalf("got (%q, %q), want (%q, %q)", key, value, "brain_provider", "codex")
	}
}

func TestPaneMatchesProviderRecognizesCodexWrappedByNode(t *testing.T) {
	if !paneMatchesProvider(ProviderCodex, "node", "cd /tmp && codex -C /tmp --model gpt-5.4-mini") {
		t.Fatal("expected codex start command under node to match provider")
	}
	if paneMatchesProvider(ProviderCodex, "zsh", "") {
		t.Fatal("did not expect unrelated pane to match codex")
	}
}

func TestFindAgentPaneStrictRequiresProjectMatch(t *testing.T) {
	if got := findAgentPaneStrict("/tmp/project", ProviderCodex); got != "" {
		t.Fatalf("expected no strict pane match in unit test environment, got %q", got)
	}
}
