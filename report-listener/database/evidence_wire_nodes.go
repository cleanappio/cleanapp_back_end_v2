package database

import (
	"encoding/json"
	"fmt"

	"cleanapp-common/wiremeta"
)

// Preserve absent SQL NULL separately from JSON null and all other JSON types.
// Both selected-source readers use this exact projection reconstruction.
func assembleEvidenceWireNodes(s *evidenceSourceNormalizationRow, nodes [][]byte) error {
	if len(nodes) != len(wiremeta.NodeColumns()) || len(nodes) != 15 {
		return fmt.Errorf("selected evidence Wire node contract changed")
	}
	platform := map[string]json.RawMessage{}
	metadata := map[string]json.RawMessage{}
	privacy := map[string]json.RawMessage{}
	put := func(m map[string]json.RawMessage, key string, node []byte) {
		if node != nil {
			m[key] = json.RawMessage(node)
		}
	}
	put(platform, "platform", nodes[0])
	put(platform, "legacy_source", nodes[1])
	put(metadata, "source_url", nodes[2])
	put(platform, "source_url", nodes[3])
	put(platform, "url", nodes[4])
	put(metadata, "source_published_at", nodes[6])
	put(metadata, "author_did", nodes[7])
	put(metadata, "author_username", nodes[8])
	put(metadata, "author_handle", nodes[9])
	put(platform, "author_handle", nodes[10])
	put(privacy, "contains_pii", nodes[11])
	put(privacy, "requires_redaction", nodes[12])
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	platform["metadata"] = metaJSON
	dcJSON, err := json.Marshal(platform)
	if err != nil {
		return err
	}
	privacyJSON, err := json.Marshal(privacy)
	if err != nil {
		return err
	}
	report := map[string]json.RawMessage{"digital_context": dcJSON, "privacy": privacyJSON}
	if nodes[5] != nil {
		bundle, err := json.Marshal([]map[string]json.RawMessage{{"uri": nodes[5]}})
		if err != nil {
			return err
		}
		report["evidence_bundle"] = bundle
	}
	s.WireReport, err = json.Marshal(report)
	if err != nil {
		return err
	}
	provenance := map[string]json.RawMessage{}
	extensions := map[string]json.RawMessage{}
	put(provenance, "generation_method", nodes[13])
	put(extensions, "observation_time_supplied", nodes[14])
	s.WireProvenance, err = json.Marshal(provenance)
	if err != nil {
		return err
	}
	s.WireExtensions, err = json.Marshal(extensions)
	return err
}
