package handlers

import (
	"net/http"

	"customer-service/database"

	"github.com/gin-gonic/gin"
)

// SyncHandlers handles synchronization endpoints
type SyncHandlers struct {
	syncService *database.SyncService
}

// NewSyncHandlers creates a new sync handlers instance
func NewSyncHandlers(syncService *database.SyncService) *SyncHandlers {
	return &SyncHandlers{
		syncService: syncService,
	}
}

// GetSyncData returns all customer data for synchronization
func (h *SyncHandlers) GetSyncData(c *gin.Context) {
	records, err := h.syncService.GetUnsyncedRecords(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get sync data"})
		return
	}

	c.JSON(http.StatusOK, records)
}

// PostSyncData receives auth data and syncs it to customers
func (h *SyncHandlers) PostSyncData(c *gin.Context) {
	var authData database.AuthData
	if err := c.ShouldBindJSON(&authData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Sync the auth data
	if err := h.syncService.SyncFromAuthService(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "sync completed successfully"})
}

// TriggerSync triggers a full synchronization
func (h *SyncHandlers) TriggerSync(c *gin.Context) {
	// Sync from auth service
	if err := h.syncService.SyncFromAuthService(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync from auth service"})
		return
	}

	// Sync to auth service
	if err := h.syncService.SyncToAuthService(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync to auth service"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "full sync completed successfully"})
}
