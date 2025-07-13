package handlers

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"customer-service/config"
	"customer-service/database"
	"customer-service/models"
	"customer-service/utils/stripe"

	"github.com/gin-gonic/gin"
)

// Handlers contains all HTTP handlers
type Handlers struct {
	service      *database.CustomerService
	stripeClient *stripe.Client
	config       *config.Config
}

// NewHandlers creates a new handlers instance
func NewHandlers(service *database.CustomerService, stripeClient *stripe.Client, cfg *config.Config) *Handlers {
	return &Handlers{
		service:      service,
		stripeClient: stripeClient,
		config:       cfg,
	}
}

// CreateCustomer handles customer registration
func (h *Handlers) CreateCustomer(c *gin.Context) {
	proxyToAuthService(c, "/api/v3/auth/register", h.config.AuthServiceURL)
}

// UpdateCustomer handles customer information updates
func (h *Handlers) UpdateCustomer(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		log.Printf("WARNING: UpdateCustomer called without customer_id from %s", c.ClientIP())
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req models.UpdateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("ERROR: Invalid JSON in UpdateCustomer request for customer %s from %s: %v", customerID, c.ClientIP(), err)
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	log.Printf("INFO: Updating customer %s with %d areas from %s", customerID, len(req.AreaIDs), c.ClientIP())

	if err := h.service.UpdateCustomer(c.Request.Context(), customerID, req); err != nil {
		log.Printf("ERROR: Failed to update customer %s from %s: %v", customerID, c.ClientIP(), err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to update customer"})
		return
	}

	log.Printf("INFO: Customer %s updated successfully from %s", customerID, c.ClientIP())
	c.JSON(http.StatusOK, models.MessageResponse{Message: "customer updated successfully"})
}

// DeleteCustomer handles customer account deletion
func (h *Handlers) DeleteCustomer(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	if err := h.service.DeleteCustomer(c.Request.Context(), customerID); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to delete customer"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "customer deleted successfully"})
}

// GetCustomer retrieves customer information
func (h *Handlers) GetCustomer(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		log.Printf("WARNING: GetCustomer called without customer_id from %s", c.ClientIP())
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	log.Printf("DEBUG: Getting customer %s from %s", customerID, c.ClientIP())

	customer, err := h.service.GetCustomer(c.Request.Context(), customerID)
	if err != nil {
		if err.Error() == "customer not found" {
			log.Printf("WARNING: Customer not found in GetCustomer - ID: %s, From: %s", customerID, c.ClientIP())
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "customer not found"})
			return
		}
		log.Printf("ERROR: Failed to get customer %s from %s: %v", customerID, c.ClientIP(), err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to get customer"})
		return
	}

	c.JSON(http.StatusOK, customer)
}

// Login handles customer authentication
func (h *Handlers) Login(c *gin.Context) {
	proxyToAuthService(c, "/api/v3/auth/login", h.config.AuthServiceURL)
}

// HealthCheck returns the service health status
func (h *Handlers) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "customer-service",
	})
}

// RefreshToken handles token refresh
func (h *Handlers) RefreshToken(c *gin.Context) {
	proxyToAuthService(c, "/api/v3/auth/refresh", h.config.AuthServiceURL)
}

// Logout handles customer logout
func (h *Handlers) Logout(c *gin.Context) {
	proxyToAuthServiceWithAuth(c, "/api/v3/auth/logout", h.config.AuthServiceURL)
}

// Remove OAuthLogin and GetOAuthURL handlers (not supported by new auth-service)

// ReactivateSubscription reactivates a canceled subscription
func (h *Handlers) ReactivateSubscription(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	subscription, err := h.service.ReactivateSubscription(c.Request.Context(), customerID)
	if err != nil {
		if err.Error() == "no canceled subscription found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to reactivate subscription"})
		return
	}

	c.JSON(http.StatusOK, subscription)
}

// DownloadInvoice returns a billing invoice
func (h *Handlers) DownloadInvoice(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	billingID := c.Param("id")

	// Get billing record and verify ownership
	billing, err := h.service.GetBillingRecord(c.Request.Context(), customerID, billingID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "invoice not found"})
		return
	}

	// Generate or retrieve invoice PDF
	invoiceData, contentType, err := h.service.GenerateInvoice(c.Request.Context(), billing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate invoice"})
		return
	}

	// Set headers for file download
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "attachment; filename=invoice-"+billingID+".pdf")
	c.Header("Content-Length", fmt.Sprint(len(invoiceData)))

	c.Data(http.StatusOK, contentType, invoiceData)
}

// GetAreas returns available service areas
func (h *Handlers) GetAreas(c *gin.Context) {
	areas, err := h.service.GetAreas(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to get areas"})
		return
	}

	c.JSON(http.StatusOK, areas)
}

func (h *Handlers) GetPrices(c *gin.Context) {
	// Retrieve all products and their prices from Stripe
	prices := models.PricesResponse{
		Prices: []models.Price{},
	}
	sp, err := h.stripeClient.GetPrices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to retrieve prices"})
		return
	}

	for key, p := range sp {
		prod_period := strings.Split(key, "_")
		prices.Prices = append(prices.Prices, models.Price{
			Product:  prod_period[0],
			Period:   prod_period[1],
			Amount:   p.UnitAmount,
			Currency: string(p.Currency),
		})
	}

	c.JSON(http.StatusOK, prices)
}

// RootHealthCheck returns service health at root level
func (h *Handlers) RootHealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// CheckUserExists checks if a user exists by email
func (h *Handlers) CheckUserExists(c *gin.Context) {
	proxyToAuthServiceQuery(c, "/api/v3/users/exists", h.config.AuthServiceURL)
}

// Utility: Proxy POST/PUT requests with JSON body to auth-service
func proxyToAuthService(c *gin.Context, path string, authServiceURL string) {
	url := authServiceURL + path
	body, _ := io.ReadAll(c.Request.Body)

	log.Printf("DEBUG: Proxying request to auth-service: %s from %s", path, c.ClientIP())

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("ERROR: Failed to proxy request to auth-service %s from %s: %v", path, c.ClientIP(), err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{Error: "auth-service unavailable"})
		return
	}
	defer resp.Body.Close()

	log.Printf("DEBUG: Auth-service response for %s: %d from %s", path, resp.StatusCode, c.ClientIP())
	copyResponse(c, resp)
}

// Utility: Proxy POST requests with Authorization header to auth-service
func proxyToAuthServiceWithAuth(c *gin.Context, path string, authServiceURL string) {
	url := authServiceURL + path
	body, _ := io.ReadAll(c.Request.Body)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if auth := c.GetHeader("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	log.Printf("DEBUG: Proxying authenticated request to auth-service: %s from %s", path, c.ClientIP())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to proxy authenticated request to auth-service %s from %s: %v", path, c.ClientIP(), err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{Error: "auth-service unavailable"})
		return
	}
	defer resp.Body.Close()

	log.Printf("DEBUG: Auth-service authenticated response for %s: %d from %s", path, resp.StatusCode, c.ClientIP())
	copyResponse(c, resp)
}

// Utility: Proxy GET requests with query params to auth-service
func proxyToAuthServiceQuery(c *gin.Context, path string, authServiceURL string) {
	url := authServiceURL + path + "?" + c.Request.URL.RawQuery

	log.Printf("DEBUG: Proxying query request to auth-service: %s from %s", path, c.ClientIP())

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("ERROR: Failed to proxy query request to auth-service %s from %s: %v", path, c.ClientIP(), err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{Error: "auth-service unavailable"})
		return
	}
	defer resp.Body.Close()

	log.Printf("DEBUG: Auth-service query response for %s: %d from %s", path, resp.StatusCode, c.ClientIP())
	copyResponse(c, resp)
}

// Utility: Copy response from auth-service to client
func copyResponse(c *gin.Context, resp *http.Response) {
	c.Status(resp.StatusCode)
	for k, v := range resp.Header {
		for _, vv := range v {
			c.Writer.Header().Add(k, vv)
		}
	}
	io.Copy(c.Writer, resp.Body)
}
