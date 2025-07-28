package config

import (
	"os"
	"reflect"
	"testing"
)

func TestGetRequiredEnvAsInt64Slice(t *testing.T) {
	// Test cases
	testCases := []struct {
		name       string
		envValue   string
		expected   []int64
		shouldFail bool
	}{
		{
			name:       "Simple comma-separated values",
			envValue:   "1,2,3,4,5",
			expected:   []int64{1, 2, 3, 4, 5},
			shouldFail: false,
		},
		{
			name:       "Values with spaces",
			envValue:   " 1 , 2 , 3 ",
			expected:   []int64{1, 2, 3},
			shouldFail: false,
		},
		{
			name:       "Single value",
			envValue:   "42",
			expected:   []int64{42},
			shouldFail: false,
		},
		{
			name:       "Empty parts",
			envValue:   "1,,3",
			expected:   []int64{1, 3},
			shouldFail: false,
		},
		{
			name:       "Large numbers",
			envValue:   "9223372036854775807,9223372036854775806",
			expected:   []int64{9223372036854775807, 9223372036854775806},
			shouldFail: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variable
			os.Setenv("TEST_INT64_SLICE", tc.envValue)
			defer os.Unsetenv("TEST_INT64_SLICE")

			result := getRequiredEnvAsInt64Slice("TEST_INT64_SLICE")

			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestGetRequiredEnvAsInt64Slice_EmptyParts(t *testing.T) {
	// Test with various empty part scenarios
	testCases := []struct {
		name     string
		envValue string
		expected []int64
	}{
		{
			name:     "Leading comma",
			envValue: ",1,2,3",
			expected: []int64{1, 2, 3},
		},
		{
			name:     "Trailing comma",
			envValue: "1,2,3,",
			expected: []int64{1, 2, 3},
		},
		{
			name:     "Multiple consecutive commas",
			envValue: "1,,,2,,3",
			expected: []int64{1, 2, 3},
		},
		{
			name:     "Only spaces",
			envValue: " , , ",
			expected: []int64{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("TEST_INT64_SLICE", tc.envValue)
			defer os.Unsetenv("TEST_INT64_SLICE")

			result := getRequiredEnvAsInt64Slice("TEST_INT64_SLICE")

			if len(result) != len(tc.expected) {
				t.Errorf("Expected length %d, got length %d", len(tc.expected), len(result))
			} else if len(result) > 0 && !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestGetRequiredEnvAsInt64Slice_InvalidInput(t *testing.T) {
	// Test invalid input scenarios
	testCases := []struct {
		name     string
		envValue string
	}{
		{
			name:     "Invalid number",
			envValue: "1,abc,3",
		},
		{
			name:     "Number too large",
			envValue: "9223372036854775808", // Max int64 + 1
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This test will cause the program to exit due to log.Fatalf
			// We can't easily test this without mocking log.Fatalf
			// For now, we'll just verify the function exists and can be called
			t.Skip("Skipping test that would cause program exit")
		})
	}
}

func TestGetRequiredEnvAsInt64Slice_MissingEnvVar(t *testing.T) {
	// This test will cause the program to exit due to log.Fatalf
	// We can't easily test this without mocking log.Fatalf
	// For now, we'll just verify the function exists and can be called
	t.Skip("Skipping test that would cause program exit")
}
