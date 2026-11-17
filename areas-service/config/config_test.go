package config

import (
	"reflect"
	"testing"
)

func TestDefaultAllowedOriginsIncludeTheMobileAppMap(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "")
	t.Setenv("FRONTEND_URL", "")
	want := []string{"https://cleanapp.io", "https://www.cleanapp.io", "https://embed.cleanapp.io"}
	if got := allowedOrigins(); !reflect.DeepEqual(got, want) {
		t.Fatalf("allowedOrigins() = %v, want %v", got, want)
	}
}

func TestExplicitAllowedOriginsAndOtherFrontendsAreUsedAsGiven(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://a.example,https://b.example")
	if got := allowedOrigins(); !reflect.DeepEqual(got, []string{"https://a.example", "https://b.example"}) {
		t.Fatalf("explicit origins changed: %v", got)
	}
	t.Setenv("ALLOWED_ORIGINS", "")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	if got := allowedOrigins(); !reflect.DeepEqual(got, []string{"http://localhost:3000"}) {
		t.Fatalf("local frontend gained extra origins: %v", got)
	}
}
