package edge

import "testing"

func TestOriginAllowed(t *testing.T) {
	if !originAllowed("https://www.cleanapp.io", []string{"https://cleanapp.io", "https://www.cleanapp.io"}) {
		t.Fatal("expected exact origin match")
	}
	if originAllowed("https://evil.example", []string{"https://cleanapp.io"}) {
		t.Fatal("unexpected origin match")
	}
}
