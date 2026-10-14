package handlers

import "testing"

func TestHumanSubmissionDoesNotInventObservationTime(t *testing.T) {
	for _, input := range []string{"", "invalid"} {
		sub := buildHumanWireSubmission(humanReportSubmissionRequest{Annotation: "I found a blocked ramp", ObservedAt: input}, "human")
		if sub.ObservedAt != "" {
			t.Fatalf("missing observation replaced with submission time: %q", sub.ObservedAt)
		}
	}
}

func TestHumanSubmissionPreservesExplicitObservationTime(t *testing.T) {
	const observed = "2025-07-20T10:44:43Z"
	sub := buildHumanWireSubmission(humanReportSubmissionRequest{Annotation: "I found a blocked ramp", ObservedAt: observed}, "human")
	if sub.ObservedAt != observed {
		t.Fatalf("explicit observation lost: %q", sub.ObservedAt)
	}
	if sub.Extensions["observation_time_supplied"] != true {
		t.Fatal("explicit observation must be distinguished from historical defaults")
	}
}

func TestShareCaptureTimeDoesNotBecomeIncidentTime(t *testing.T) {
	sub := buildDigitalShareWireSubmission(normalizedDigitalSharePayload{ClientCreatedAt: "2025-07-20T10:44:43Z", SharedText: "I hit an error"}, "human")
	if sub.ObservedAt != "" {
		t.Fatalf("share capture time presented as incident time: %q", sub.ObservedAt)
	}
}
