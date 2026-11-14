package config

import "testing"

func TestExternalDigitalEnrichmentConfigPreservesDefaultAndCanBeDisabled(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DB_PASSWORD", "stub")
	t.Setenv("AMQP_USER", "stub")
	t.Setenv("AMQP_PASSWORD", "stub")
	t.Setenv("ANALYZER_LLM_PROVIDER", "stub")

	for _, tc := range []struct {
		name     string
		value    string
		evidence string
		want     bool
	}{
		{name: "default preserves existing enrichment", evidence: "false", want: true},
		{name: "explicit enable remains independent of evidence polling", value: "true", evidence: "false", want: true},
		{name: "secondary worker disables global enrichment only", value: "false", evidence: "true", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENRICH_EXTERNAL_DIGITAL_ENABLED", tc.value)
			t.Setenv("EVIDENCE_WORKER_ENABLED", tc.evidence)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("load analyzer config: %v", err)
			}
			if cfg.EnrichExternalDigitalEnabled != tc.want {
				t.Fatalf("global enrichment enabled = %v, want %v", cfg.EnrichExternalDigitalEnabled, tc.want)
			}
			if cfg.EvidenceWorkerEnabled != (tc.evidence == "true") {
				t.Fatal("global enrichment flag changed evidence polling")
			}
		})
	}
}
