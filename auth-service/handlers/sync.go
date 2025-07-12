package handlers

import (
	"net/http"

	"auth-service/database"

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

// GetSyncData returns all client_auth data for synchronization
func (h *SyncHandlers) GetSyncData(c *gin.Context) {
	records, err := h.syncService.GetUnsyncedRecords(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get sync data"})
		return
	}

	c.JSON(http.StatusOK, records)
}

// PostSyncData receives customer data and syncs it to client_auth
func (h *SyncHandlers) PostSyncData(c *gin.Context) {
	var customerData database.CustomerData
	if err := c.ShouldBindJSON(&customerData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Sync the customer data
	if err := h.syncService.SyncFromCustomerService(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "sync completed successfully"})
}

// TriggerSync triggers a full synchronization
func (h *SyncHandlers) TriggerSync(c *gin.Context) {
	// Sync from customer service
	if err := h.syncService.SyncFromCustomerService(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync from customer service"})
		return
	}

	// Sync to customer service
	if err := h.syncService.SyncToCustomerService(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync to customer service"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "full sync completed successfully"})
}
