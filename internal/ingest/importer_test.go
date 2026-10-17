package ingest

import "testing"

func TestNew_RejectsInvalidConfig(t *testing.T) {
	f := newFixture(t)
	for name, mutate := range map[string]func(*Config){
		"rejects_zero_threshold":      func(c *Config) { c.SimilarityThreshold = 0 },
		"rejects_threshold_above_one": func(c *Config) { c.SimilarityThreshold = 1.1 },
		"rejects_zero_batch_size":     func(c *Config) { c.BatchSize = 0 },
		"rejects_missing_translator":  func(c *Config) { c.Translator = nil },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := defaultConfig(t)
			mutate(&cfg)
			if _, err := New(f.store, cfg); err == nil {
				t.Error("New succeeded, want an error")
			}
		})
	}
}
