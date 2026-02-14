package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type promoteReportReq struct {
	Visibility string `json:"visibility"`
	TrustLevel string `json:"trust_level"`
}

func (h *Handlers) InternalPromoteReport(c *gin.Context) {
	seqStr := c.Param("seq")
	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid seq"})
		return
	}

	var req promoteReportReq
	_ = c.ShouldBindJSON(&req)

	vis := strings.ToLower(strings.TrimSpace(req.Visibility))
	if vis == "" {
		vis = "public"
	}
	switch vis {
	case "shadow", "limited", "public":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid visibility"})
		return
	}

	trust := strings.ToLower(strings.TrimSpace(req.TrustLevel))
	if trust == "" {
		trust = "verified"
	}
	switch trust {
	case "unverified", "verified", "trusted":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid trust_level"})
		return
	}

	_, err = h.db.DB().ExecContext(c.Request.Context(), `
		INSERT INTO report_raw (report_seq, visibility, trust_level)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE visibility=VALUES(visibility), trust_level=VALUES(trust_level), updated_at=NOW()
	`, seq, vis, trust)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"seq": seq, "visibility": vis, "trust_level": trust})
}

type suspendFetcherReq struct {
	Status string `json:"status"`
}

func (h *Handlers) InternalSuspendFetcher(c *gin.Context) {
	fetcherID := strings.TrimSpace(c.Param("fetcher_id"))
	if fetcherID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fetcher_id"})
		return
	}
	var req suspendFetcherReq
	_ = c.ShouldBindJSON(&req)

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status == "" {
		status = "suspended"
	}
	switch status {
	case "limited", "suspended", "banned":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	_, err := h.db.DB().ExecContext(c.Request.Context(), `
		UPDATE fetchers SET status=?, active=FALSE WHERE fetcher_id=?
	`, status, fetcherID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"fetcher_id": fetcherID, "status": status})
}

func (h *Handlers) InternalRevokeFetcherKey(c *gin.Context) {
	keyID := strings.TrimSpace(c.Param("key_id"))
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key_id"})
		return
	}
	_, err := h.db.DB().ExecContext(c.Request.Context(), `
		UPDATE fetcher_keys SET status='revoked' WHERE key_id=?
	`, keyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key_id": keyID, "status": "revoked"})
}
