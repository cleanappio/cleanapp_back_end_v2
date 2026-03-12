package database

import (
	"strings"
	"testing"
)

func TestInsertCaseSQLPlaceholderCountMatchesColumns(t *testing.T) {
	columnsSection := between(insertCaseSQL, "(", ") VALUES")
	columnCount := 0
	for _, column := range strings.Split(columnsSection, ",") {
		if strings.TrimSpace(column) != "" {
			columnCount++
		}
	}

	placeholderCount := strings.Count(insertCaseSQL, "?")
	if columnCount != placeholderCount {
		t.Fatalf("insertCaseSQL has %d columns but %d placeholders", columnCount, placeholderCount)
	}
}

func between(value, start, end string) string {
	startIdx := strings.Index(value, start)
	if startIdx < 0 {
		return ""
	}
	startIdx += len(start)
	endIdx := strings.Index(value[startIdx:], end)
	if endIdx < 0 {
		return value[startIdx:]
	}
	return value[startIdx : startIdx+endIdx]
}
