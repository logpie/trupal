package main

import "testing"

func TestConfigValidateNormalizesBrainSettings(t *testing.T) {
	cfg := Config{
		BrainProvider: " CLAUDE ",
		BrainModel:    " SONNET ",
		BrainEffort:   " HIGH ",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
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
				BrainProvider: "claude",
				BrainModel:    "invalid",
				BrainEffort:   "high",
			},
		},
		{
			name: "invalid effort",
			cfg: Config{
				BrainProvider: "claude",
				BrainModel:    "sonnet",
				BrainEffort:   "turbo",
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
