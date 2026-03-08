package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"report-listener/database"
)

func legacyWireReceiptMetadataFromRecord(receipt *database.WireReceipt) legacyWireReceiptMetadata {
	meta := legacyWireReceiptMetadata{
		SourceID:     receipt.SourceID,
		ReceiptID:    receipt.ReceiptID,
		SubmissionID: receipt.SubmissionID,
		Status:       receipt.Status,
		Lane:         receipt.Lane,
	}
	if receipt.ReportSeq.Valid {
		meta.ReportSeq = int(receipt.ReportSeq.Int64)
	}
	if receipt.NextCheckAfter.Valid {
		meta.NextCheckAfter = receipt.NextCheckAfter.Time.UTC().Format(time.RFC3339)
	}
	return meta
}

func (h *Handlers) mirrorLegacyBulkIngestToWire(ctx context.Context, fetcherID, source string, items []preparedBulkIngestItem) ([]legacyWireReceiptMetadata, error) {
	fetcher, err := h.db.GetFetcherV1ByID(ctx, fetcherID)
	if err != nil {
		return nil, fmt.Errorf("lookup fetcher: %w", err)
	}

	metadata := make([]legacyWireReceiptMetadata, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.ext) == "" {
			continue
		}
		if _, err := h.db.GetWireSubmissionByFetcherAndSource(ctx, fetcherID, it.ext); err == nil {
			receipt, rerr := h.db.GetLatestWireReceiptBySource(ctx, fetcherID, it.ext)
			if rerr == nil {
				metadata = append(metadata, legacyWireReceiptMetadataFromRecord(receipt))
			}
			continue
		} else if err != sql.ErrNoRows {
			return nil, fmt.Errorf("lookup existing wire submission: %w", err)
		}

		sub := cleanAppWireSubmission{
			SchemaVersion: cleanAppWireSchemaV1,
			SourceID:      it.ext,
			SubmittedAt:   time.Now().UTC().Format(time.RFC3339),
			ObservedAt:    it.createdAt,
		}
		sub.Agent.AgentID = "legacy-bulk-ingest-" + normalizeWireSlug(source)
		sub.Agent.AgentName = "Legacy Bulk Ingest"
		sub.Agent.AgentType = "fetcher"
		sub.Agent.OperatorType = fetcher.OwnerType
		sub.Agent.AuthMethod = "legacy_static_token"
		sub.Agent.SoftwareVersion = "compat"
		sub.Agent.ExecutionMode = "legacy_route"
		sub.Provenance.GenerationMethod = "legacy_v3_bulk_ingest"
		sub.Provenance.UpstreamSources = []struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}{
			{Kind: "legacy_source", Value: source},
			{Kind: "external_id", Value: it.ext},
		}
		sub.Provenance.ChainOfCustody = []string{"legacy_v3_bulk_ingest", "wire_mirror"}
		sub.Report.Domain = it.classification
		if sub.Report.Domain == "" {
			sub.Report.Domain = "digital"
		}
		sub.Report.ProblemType = normalizeWireSlug(source)
		if sub.Report.ProblemType == "" {
			sub.Report.ProblemType = "legacy_machine_report"
		}
		sub.Report.Title = it.title
		sub.Report.Description = it.description
		sub.Report.Language = it.lang
		sub.Report.Confidence = clamp01(it.severity)
		sub.Report.TargetEntity.TargetType = "brand"
		sub.Report.TargetEntity.Name = it.brandDisplay
		sub.Report.DigitalContext = map[string]any{
			"legacy_source": source,
			"url":           it.url,
			"summary":       it.summary,
			"brand_name":    it.brandName,
		}
		if strings.EqualFold(sub.Report.Domain, "physical") || it.lat != 0 || it.lon != 0 {
			sub.Report.Location = &struct {
				Kind            string  `json:"kind,omitempty"`
				Lat             float64 `json:"lat"`
				Lng             float64 `json:"lng"`
				Geohash         string  `json:"geohash,omitempty"`
				AddressText     string  `json:"address_text,omitempty"`
				PlaceConfidence float64 `json:"place_confidence,omitempty"`
			}{
				Kind:            "coordinate",
				Lat:             it.lat,
				Lng:             it.lon,
				PlaceConfidence: 0.7,
			}
			sub.Report.DigitalContext = nil
		}
		if strings.TrimSpace(it.url) != "" {
			sub.Report.EvidenceBundle = append(sub.Report.EvidenceBundle, struct {
				EvidenceID string `json:"evidence_id,omitempty"`
				Type       string `json:"type"`
				URI        string `json:"uri,omitempty"`
				SHA256     string `json:"sha256,omitempty"`
				MIMEType   string `json:"mime_type,omitempty"`
				CapturedAt string `json:"captured_at,omitempty"`
			}{
				EvidenceID: "legacy_url_" + strings.ReplaceAll(it.ext, ":", "_"),
				Type:       "url",
				URI:        it.url,
				CapturedAt: it.createdAt,
			})
		}
		sub.Delivery.RequestedLane = "auto"
		sub, generatedSubmissionID := normalizeCleanAppWireSubmission(sub)
		materialHash, err := cleanAppWireMaterialHash(sub)
		if err != nil {
			return nil, fmt.Errorf("material hash: %w", err)
		}
		quality := computeCleanAppWireSubmissionQuality(sub)
		lane := assignCleanAppWireLane(h.cfg, fetcher.Tier, quality, len(sub.Report.EvidenceBundle), sub.Delivery.RequestedLane)

		now := time.Now().UTC()
		receiptID := newReceiptID()
		submissionRecord := &database.WireSubmissionRaw{
			SubmissionID:      generatedSubmissionID,
			ReceiptID:         receiptID,
			FetcherID:         fetcherID,
			SourceID:          sub.SourceID,
			SchemaVersion:     sub.SchemaVersion,
			SubmittedAt:       mustRFC3339(sub.SubmittedAt),
			ObservedAt:        nullableTimeValue(sub.ObservedAt),
			AgentID:           sub.Agent.AgentID,
			Lane:              lane,
			MaterialHash:      materialHash,
			SubmissionQuality: quality,
			ReportSeq:         sql.NullInt64{Int64: int64(it.seq), Valid: it.seq > 0},
			AgentJSON:         h.db.MarshalJSON(sub.Agent),
			ProvenanceJSON:    h.db.MarshalJSON(sub.Provenance),
			ReportJSON:        h.db.MarshalJSON(sub.Report),
			DedupeJSON:        h.db.MarshalJSON(sub.Dedupe),
			DeliveryJSON:      h.db.MarshalJSON(sub.Delivery),
			ExtensionsJSON:    h.db.MarshalJSON(map[string]any{"legacy_source": source, "compat_mode": "v3_bulk_ingest"}),
		}
		warnings := cleanAppWireWarningsForSubmission(sub, lane)
		warnings = append(warnings, "legacy_v3_route_mirrored")
		receiptRecord := &database.WireReceipt{
			ReceiptID:         receiptID,
			SubmissionID:      generatedSubmissionID,
			FetcherID:         fetcherID,
			SourceID:          sub.SourceID,
			ReportSeq:         sql.NullInt64{Int64: int64(it.seq), Valid: it.seq > 0},
			Status:            laneToStatus(lane),
			Lane:              lane,
			IdempotencyReplay: false,
			WarningsJSON:      h.db.MarshalJSON(warnings),
			NextCheckAfter:    sql.NullTime{Time: now.Add(2 * time.Minute), Valid: true},
		}
		if err := h.db.InsertWireSubmissionAndReceipt(ctx, submissionRecord, receiptRecord); err != nil {
			return nil, fmt.Errorf("insert mirrored wire submission: %w", err)
		}
		metadata = append(metadata, legacyWireReceiptMetadataFromRecord(receiptRecord))
		_ = h.db.EnsureWireReputationProfile(ctx, fetcherID)
		_ = h.db.IncrementWireReputationSample(ctx, fetcherID)
	}

	return metadata, nil
}
