package config

import "testing"

func TestAgentOutcomeRecoveryDefaultsToMachineQueueAndCanBeDisabled(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DB_PASSWORD", "stub")
	t.Setenv("AMQP_USER", "stub")
	t.Setenv("AMQP_PASSWORD", "stub")
	t.Setenv("ANALYZER_LLM_PROVIDER", "stub")
	for _, tc := range []struct {
		name, queue, enabled string
		want                 bool
	}{
		{"machine queue", "report-analysis-queue", "", true},
		{"human queue", "report-analysis-human-queue", "", false},
		{"legacy default queue", "report-analyze", "", false},
		{"explicitly disabled", "report-analysis-queue", "false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RABBITMQ_QUEUE", tc.queue)
			t.Setenv("AGENT_OUTCOME_WORKER_ENABLED", tc.enabled)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AgentOutcomeWorkerEnabled != tc.want {
				t.Fatalf("agent recovery enabled=%v, want %v", cfg.AgentOutcomeWorkerEnabled, tc.want)
			}
		})
	}
}
