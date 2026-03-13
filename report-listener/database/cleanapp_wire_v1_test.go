package database

import "testing"

func TestWireSubmissionInsertPlaceholderCount(t *testing.T) {
	t.Parallel()

	const expectedArgs = 23

	if got := wireSubmissionInsertPlaceholderCount(); got != expectedArgs {
		t.Fatalf("wire submission insert placeholder count = %d, want %d", got, expectedArgs)
	}
}
