package publicid

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	ReportPrefix     = "rpt_"
	ReportTokenBytes = 16
	MaxLength        = 32
)

// NewReportID returns a non-sequential public identifier for reports.
func NewReportID() (string, error) {
	var token [ReportTokenBytes]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return ReportPrefix + base64.RawURLEncoding.EncodeToString(token[:]), nil
}

func IsReportID(id string) bool {
	if !strings.HasPrefix(id, ReportPrefix) {
		return false
	}
	if len(id) != len(ReportPrefix)+base64.RawURLEncoding.EncodedLen(ReportTokenBytes) {
		return false
	}
	for _, ch := range id[len(ReportPrefix):] {
		switch {
		case ch >= 'A' && ch <= 'Z':
		case ch >= 'a' && ch <= 'z':
		case ch >= '0' && ch <= '9':
		case ch == '-' || ch == '_':
		default:
			return false
		}
	}
	return true
}
