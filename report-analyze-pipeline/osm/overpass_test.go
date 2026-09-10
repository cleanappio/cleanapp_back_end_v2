package osm

import (
	"reflect"
	"testing"
)

func TestGenerateHierarchyEmailsOnlyUsesTrustedDomainsForInference(t *testing.T) {
	hierarchy := []HierarchyLevel{
		{Name: "School", Type: "school", Domain: "law.ucla.edu"},
		{Name: "City", Type: "city", Domain: "city.ch"},
		{Name: "District Office", Type: "organization", Domain: "district.org", ContactEmail: "office@district.org"},
	}

	got := GenerateHierarchyEmails(hierarchy)
	want := []string{
		"facilities@law.ucla.edu",
		"maintenance@law.ucla.edu",
		"office@law.ucla.edu",
		"office@district.org",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GenerateHierarchyEmails() = %v, want %v", got, want)
	}
}

func TestFilterHighConfidencePhysicalEmailsRejectsLowConfidenceAliases(t *testing.T) {
	input := []string{
		"publicworks@city.ch",
		"info@municipality.gov",
		"facilities@campus.edu",
		"maintenance@law.ucla.edu",
		"office@school.ch",
		"principal@school.ch",
		"principal@school.ch",
	}

	got := FilterHighConfidencePhysicalEmails(input)
	want := []string{
		"facilities@campus.edu",
		"maintenance@law.ucla.edu",
		"office@school.ch",
		"principal@school.ch",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterHighConfidencePhysicalEmails() = %v, want %v", got, want)
	}
}
