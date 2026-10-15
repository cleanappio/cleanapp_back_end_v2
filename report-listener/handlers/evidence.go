package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"report-listener/database"
	"report-listener/models"

	"github.com/gin-gonic/gin"
)

func parseEvidenceBrandFilter(c *gin.Context) (database.EvidenceFilter, error) {
	kind := c.Query("kind")
	if kind == "" {
		kind = c.Query("content_kind")
	}
	n, err := parseOptionalInt(c.Query("n"), 12)
	if err != nil || n <= 0 {
		return database.EvidenceFilter{}, fmt.Errorf("n must be a positive integer")
	}
	offset, err := parseOptionalInt(c.Query("offset"), 0)
	if err != nil {
		return database.EvidenceFilter{}, fmt.Errorf("offset must be an integer")
	}
	return database.NormalizeEvidenceFilter(database.EvidenceFilter{BrandName: c.Query("brand_name"), Product: c.Query("product"), Kind: kind, Sort: c.Query("sort"), Language: c.DefaultQuery("lang", "en"), Limit: n, Offset: offset})
}

func (h *Handlers) serveEvidenceBrand(c *gin.Context, discovery bool) {
	filter, err := parseEvidenceBrandFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fullData := false
	if !discovery {
		fullData, err = strconv.ParseBool(c.DefaultQuery("full_data", "false"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "full_data must be true or false"})
			return
		}
	}
	page, err := h.db.GetEvidenceBrandPage(c.Request.Context(), filter)
	if err != nil {
		log.Printf("Evidence brand briefing failed for %q: %v", filter.BrandName, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Evidence briefing temporarily unavailable"})
		return
	}
	scope := filter.Scope()
	if discovery {
		items, err := toPublicDiscoveryCards(page.Reports, filter.Language, h.sealDiscoveryReportToken)
		if err != nil {
			log.Printf("Evidence discovery cards failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve evidence briefing"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "count": len(items), "total_count": page.Total, "high_priority_count": page.High, "medium_priority_count": page.Medium, "kind": filter.Kind, "sort": filter.Sort, "offset": filter.Offset, "has_more": page.HasMore, "kind_counts": page.KindCounts, "counts_exact": true, "scope": scope, "overview": page.Overview})
		return
	}
	response := gin.H{"reports": page.Reports, "count": len(page.Reports), "total_count": page.Total, "high_priority_count": page.High, "medium_priority_count": page.Medium, "kind": filter.Kind, "sort": filter.Sort, "offset": filter.Offset, "has_more": page.HasMore, "kind_counts": page.KindCounts, "counts_exact": true, "scope": scope, "overview": page.Overview, "from_seq": 0, "to_seq": 0}
	if len(page.Reports) > 0 {
		response["from_seq"] = page.Reports[0].Report.Seq
		response["to_seq"] = page.Reports[len(page.Reports)-1].Report.Seq
	}
	if fullData {
		seqs := make([]int, len(page.Reports))
		for i, r := range page.Reports {
			seqs[i] = r.Report.Seq
		}
		reports := []models.ReportWithAnalysis{}
		if len(seqs) > 0 {
			reports, err = h.db.GetReportsBySeqs(c.Request.Context(), seqs)
			if err != nil {
				log.Printf("Evidence full reports failed: %v", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Evidence briefing temporarily unavailable"})
				return
			}
			bySeq := make(map[int]models.ReportWithAnalysis, len(reports))
			for _, r := range reports {
				bySeq[r.Report.Seq] = r
			}
			ordered := make([]models.ReportWithAnalysis, 0, len(page.Reports))
			for _, r := range page.Reports {
				if full, ok := bySeq[r.Report.Seq]; ok {
					full.Evidence = r.Evidence
					full.Recurrence = r.Recurrence
					full.Report.SourceURL = r.Report.SourceURL
					full.Report.SourceTimestamp = r.Report.SourceTimestamp
					ordered = append(ordered, full)
				}
			}
			reports = ordered
		}
		response["reports"] = reports
		response["count"] = len(reports)
	}
	c.JSON(http.StatusOK, response)
}

// Evidence reads intentionally bypass legacy unfiltered count caches.
