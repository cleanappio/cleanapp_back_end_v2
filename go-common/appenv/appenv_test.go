package appenv

import (
	"os"
	"testing"
)

func TestCurrentDefaultsToProductionOutsideCI(t *testing.T) {
	_ = os.Unsetenv("APP_ENV")
	_ = os.Unsetenv("CI")
	if got := Current(); got != EnvironmentProduction {
		t.Fatalf("expected production, got %s", got)
	}
}
