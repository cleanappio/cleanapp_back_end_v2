package reportoutcome

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func words(value string) []string {
	value = strings.NewReplacer("can't", "cannot", "won't", "cannot", "log in", "login", "sign in", "login").Replace(strings.ToLower(value))
	parts := strings.FieldsFunc(value, func(char rune) bool { return !unicode.IsLetter(char) && !unicode.IsDigit(char) })
	stop := " a an and are as at be been being but by do does for from had has have i in is it its me my of on or our that the their there they this to was we were with you your "
	unique := map[string]bool{}
	for _, word := range parts {
		if len(word) < 2 || strings.Contains(stop, " "+word+" ") {
			continue
		}
		if len(word) > 5 && strings.HasSuffix(word, "ing") {
			word = strings.TrimSuffix(word, "ing")
		}
		if len(word) > 4 && strings.HasSuffix(word, "ed") {
			word = strings.TrimSuffix(word, "ed")
		}
		if len(word) > 3 && strings.HasSuffix(word, "s") {
			word = strings.TrimSuffix(word, "s")
		}
		unique[word] = true
		if len(unique) >= 96 {
			break
		}
	}
	result := make([]string, 0, len(unique))
	for word := range unique {
		result = append(result, word)
	}
	sort.Strings(result)
	return result
}

func retainedContext(raw []byte) bool {
	var report map[string]any
	if json.Unmarshal(raw, &report) != nil {
		return false
	}
	useful := func(value any) bool {
		text, ok := value.(string)
		if !ok {
			return false
		}
		text = strings.TrimSpace(strings.ToLower(text))
		return text != "" && text != "unknown" && text != "unspecified" && text != "n/a" && text != "none"
	}
	if context, ok := report["digital_context"].(map[string]any); ok {
		for _, key := range []string{"url", "source_url", "product_or_site", "product", "app", "application", "service", "host", "domain", "location_text", "source", "publisher"} {
			if useful(context[key]) {
				return true
			}
		}
	}
	if target, ok := report["target_entity"].(map[string]any); ok && useful(target["name"]) {
		return true
	}
	if location, ok := report["location"].(map[string]any); ok {
		_, lat := location["lat"].(float64)
		_, lng := location["lng"].(float64)
		if lat && lng || useful(location["address_text"]) {
			return true
		}
	}
	if bundle, ok := report["evidence_bundle"].([]any); ok {
		for _, entry := range bundle {
			item, ok := entry.(map[string]any)
			if ok && useful(item["uri"]) {
				return true
			}
		}
	}
	return false
}

func observationKeys(source source, analysis Analysis) (string, string, []string) {
	var report struct {
		Description    string         `json:"description"`
		DigitalContext map[string]any `json:"digital_context"`
		Location       *struct {
			Lat     float64 `json:"lat"`
			Lng     float64 `json:"lng"`
			Address string  `json:"address_text"`
		} `json:"location"`
	}
	if json.Unmarshal(source.reportJSON, &report) != nil {
		return "", "", nil
	}
	text := report.Description
	if strings.TrimSpace(text) == "" {
		text = source.description
	}
	context := strings.Join(words(analysis.BrandName), " ")
	if context == "" {
		for _, key := range []string{"product_or_site", "url", "source_url"} {
			value, _ := report.DigitalContext[key].(string)
			if value == "" {
				continue
			}
			if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
				value = parsed.Hostname()
			}
			context = strings.Join(words(value), " ")
			if context != "" {
				break
			}
		}
	}
	if analysis.Classification == "physical" {
		if report.Location != nil {
			context += "|" + fmt.Sprintf("%.4f,%.4f", report.Location.Lat, report.Location.Lng)
		} else {
			location, _ := report.DigitalContext["location_text"].(string)
			if location != "" {
				context += "|" + strings.Join(words(location), " ")
			}
		}
	}
	if context == "" || strings.TrimSpace(text) == "" {
		return "", "", nil
	}
	normalized := words(text)
	tokens := make([]string, 0, len(normalized))
	for _, word := range normalized {
		tokens = append(tokens, digestText(word))
	}
	sort.Strings(tokens)
	key := digestText(analysis.Classification + "|" + context)
	return key, digestText(key + "|" + strings.Join(normalized, " ")), tokens
}

const publicCandidateSQL = `r.public_id<>''
 AND NOT EXISTS(SELECT 1 FROM report_raw rr WHERE rr.report_seq=r.seq AND rr.visibility IS NOT NULL AND rr.visibility<>'public')
 AND NOT EXISTS(SELECT 1 FROM report_status rs WHERE rs.seq=r.seq AND rs.status IS NOT NULL AND rs.status<>'active')
 AND NOT EXISTS(SELECT 1 FROM reports_owners ro WHERE ro.seq=r.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)
 AND NOT EXISTS(SELECT 1 FROM wire_submissions_raw w WHERE w.report_seq=r.seq AND
 (JSON_EXTRACT(w.report_json,'$.privacy.contains_pii')=TRUE OR JSON_EXTRACT(w.report_json,'$.privacy.requires_redaction')=TRUE))
 AND NOT EXISTS(SELECT 1 FROM wire_evidence_projection h WHERE h.report_seq=r.seq AND (h.contains_pii=TRUE OR h.requires_redaction=TRUE))
 AND NOT EXISTS(SELECT 1 FROM report_evidence_source_projection p WHERE p.seq=r.seq AND p.privacy_restricted=TRUE)`

func matchCandidates(ctx context.Context, q queryer, seq int, contextKey, fingerprint string, tokens []string) ([]Candidate, error) {
	result := []Candidate{}
	if contextKey == "" || fingerprint == "" {
		return result, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT c.observation_fingerprint,c.observation_tokens,r.public_id,
 (SELECT a.classification FROM report_analysis a WHERE a.seq=r.seq AND a.language='en' ORDER BY a.updated_at DESC,a.source LIMIT 1)
 FROM (SELECT report_seq,observation_fingerprint,observation_tokens FROM report_outcomes FORCE INDEX(idx_report_outcome_context)
 WHERE context_key=? AND publication_status='public' AND report_seq<>?
 ORDER BY updated_at DESC,report_seq DESC LIMIT 25) c
 STRAIGHT_JOIN reports r ON r.seq=c.report_seq WHERE `+publicCandidateSQL, contextKey, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var previous, publicID, classification string
		var raw []byte
		if err = rows.Scan(&previous, &raw, &publicID, &classification); err != nil {
			return nil, err
		}
		var other []string
		if json.Unmarshal(raw, &other) != nil {
			continue
		}
		reason := "matching_observation"
		if previous != fingerprint {
			shared := 0
			set := map[string]bool{}
			for _, token := range other {
				set[token] = true
			}
			for _, token := range tokens {
				if set[token] {
					shared++
				}
			}
			if shared < 3 || 2*float64(shared)/float64(len(tokens)+len(other)) < 0.65 {
				continue
			}
			reason = "similar_observation"
		}
		path := canonicalPath(classification, publicID)
		if path == "" {
			continue
		}
		result = append(result, Candidate{Type: "report", Reference: publicID, CanonicalPath: path, ReasonCode: reason})
		if len(result) == 3 {
			break
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Reference < result[j].Reference })
	return result, nil
}

func visibleCandidates(ctx context.Context, q queryer, candidates []Candidate) ([]Candidate, error) {
	result := []Candidate{}
	for index, candidate := range candidates {
		if index >= 3 {
			break
		}
		if candidate.Type != "report" || candidate.Reference == "" {
			continue
		}
		var visible bool
		if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM reports r WHERE r.public_id=? AND `+publicCandidateSQL+`)`, candidate.Reference).Scan(&visible); err != nil {
			return nil, err
		}
		if visible {
			result = append(result, candidate)
		}
	}
	return result, nil
}
