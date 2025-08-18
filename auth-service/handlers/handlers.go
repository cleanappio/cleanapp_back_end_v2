package handlers

import (
	"log"
	"net/http"

	"auth-service/database"
	"auth-service/models"

	"github.com/gin-gonic/gin"
)

// Handlers handles HTTP requests for the authentication service
type Handlers struct {
	service *database.AuthService
}

// NewHandlers creates a new handlers instance
func NewHandlers(service *database.AuthService) *Handlers {
	return &Handlers{
		service: service,
	}
}

// CreateUser handles user registration
func (h *Handlers) CreateUser(c *gin.Context) {
	var req models.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("ERROR: Invalid JSON in CreateUser request from %s: %v", c.ClientIP(), err)
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	user, err := h.service.CreateUser(c.Request.Context(), req)
	if err != nil {
		if err.Error() == "user already exists" {
			log.Printf("WARNING: User creation failed - user already exists for email %s from %s", req.Email, c.ClientIP())
			c.JSON(http.StatusConflict, models.ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("ERROR: Failed to create user for email %s from %s: %v", req.Email, c.ClientIP(), err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to create user"})
		return
	}

	log.Printf("INFO: User created successfully - ID: %s, Email: %s, From: %s", user.ID, req.Email, c.ClientIP())
	c.JSON(http.StatusCreated, user)
}

// UpdateUser handles user information updates
func (h *Handlers) UpdateUser(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req models.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	user, err := h.service.UpdateUser(c.Request.Context(), userID, req)
	if err != nil {
		if err.Error() == "user not found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		if err.Error() == "email already taken" {
			c.JSON(http.StatusConflict, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to update user"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// DeleteUser handles user deletion
func (h *Handlers) DeleteUser(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	if err := h.service.DeleteUser(c.Request.Context(), userID); err != nil {
		if err.Error() == "user not found" {
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to delete user"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "user deleted successfully"})
}

// GetUser retrieves user information
func (h *Handlers) GetUser(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		log.Printf("WARNING: GetUser called without user_id from %s", c.ClientIP())
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), userID)
	if err != nil {
		if err.Error() == "user not found" {
			log.Printf("WARNING: User not found in GetUser - ID: %s, From: %s", userID, c.ClientIP())
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("ERROR: Failed to get user %s from %s: %v", userID, c.ClientIP(), err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to get user"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// Login handles user authentication
func (h *Handlers) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("ERROR: Invalid JSON in Login request from %s: %v", c.ClientIP(), err)
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// Authenticate and get user ID
	userID, err := h.service.Login(c.Request.Context(), req)
	if err != nil {
		log.Printf("WARNING: Login failed for email %s from %s: %v", req.Email, c.ClientIP(), err)
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: err.Error()})
		return
	}

	// Generate token pair
	token, refreshToken, err := h.service.GenerateTokenPair(c.Request.Context(), userID)
	if err != nil {
		log.Printf("ERROR: Failed to generate tokens for user %s from %s: %v", userID, c.ClientIP(), err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate tokens"})
		return
	}

	log.Printf("INFO: Login successful for user %s (email: %s) from %s", userID, req.Email, c.ClientIP())
	c.JSON(http.StatusOK, models.TokenResponse{
		Token:        token,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600, // 1 hour
	})
}

// HealthCheck returns the service health status
func (h *Handlers) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "auth-service",
	})
}

// RefreshToken handles token refresh
func (h *Handlers) RefreshToken(c *gin.Context) {
	var req models.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// Validate refresh token and get user ID
	userID, err := h.service.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "invalid refresh token"})
		return
	}

	// Generate new access token
	token, refreshToken, err := h.service.GenerateTokenPair(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, models.TokenResponse{
		Token:        token,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600, // 1 hour
	})
}

// Logout handles user logout
func (h *Handlers) Logout(c *gin.Context) {
	userID := c.GetString("user_id")
	token := c.GetString("token")

	if userID == "" || token == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	// Invalidate the token
	if err := h.service.InvalidateToken(c.Request.Context(), userID, token); err != nil {
		// Log error but still return success to client
		c.JSON(http.StatusOK, models.MessageResponse{Message: "logged out successfully"})
		return
	}

	c.JSON(http.StatusOK, models.MessageResponse{Message: "logged out successfully"})
}

// ValidateToken handles token validation requests from other services
func (h *Handlers) ValidateToken(c *gin.Context) {
	var req models.ValidateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("ERROR: Invalid JSON in ValidateToken request from %s: %v", c.ClientIP(), err)
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	// Validate token and get user ID
	userID, err := h.service.ValidateToken(req.Token)
	if err != nil {
		log.Printf("WARNING: Token validation failed from %s: %v", c.ClientIP(), err)
		c.JSON(http.StatusOK, models.ValidateTokenResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.ValidateTokenResponse{
		Valid:  true,
		UserID: userID,
	})
}

// CheckUserExists checks if a user exists by email
func (h *Handlers) CheckUserExists(c *gin.Context) {
	email := c.Query("email")
	if email == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "email parameter is required"})
		return
	}

	exists, err := h.service.UserExistsByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to check user existence"})
		return
	}

	c.JSON(http.StatusOK, models.UserExistsResponse{
		UserExists: exists,
	})
}

// GetUserByID retrieves user information by ID (for internal service calls)
func (h *Handlers) GetUserByID(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		log.Printf("WARNING: GetUserByID called without user ID from %s", c.ClientIP())
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "user ID is required"})
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), userID)
	if err != nil {
		if err.Error() == "user not found" {
			log.Printf("WARNING: User not found in GetUserByID - ID: %s, From: %s", userID, c.ClientIP())
			c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("ERROR: Failed to get user by ID %s from %s: %v", userID, c.ClientIP(), err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to get user"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// RootHealthCheck returns the service health status (root level)
func (h *Handlers) RootHealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "auth-service",
		"version": "1.0.0",
	})
}

// CheckReportAuthorization checks if the logged-in user is authorized to view specific reports
func (h *Handlers) CheckReportAuthorization(c *gin.Context) {
	customerID := c.GetString("customer_id")
	if customerID == "" {
		log.Printf("WARNING: CheckReportAuthorization called without customer_id from %s", c.ClientIP())
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
		return
	}

	var req models.ReportAuthorizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("ERROR: Invalid JSON in CheckReportAuthorization request for customer %s from %s: %v", customerID, c.ClientIP(), err)
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	log.Printf("INFO: Checking report authorization for customer %s with %d reports from %s", customerID, len(req.ReportSeqs), c.ClientIP())

	// Get the report authorization service from the main service
	reportAuthService := h.service.GetReportAuthService()
	if reportAuthService == nil {
		log.Printf("ERROR: Report authorization service not available for customer %s from %s", customerID, c.ClientIP())
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "report authorization service not available"})
		return
	}

	authorizations, err := reportAuthService.CheckReportAuthorization(c.Request.Context(), customerID, req.ReportSeqs)
	if err != nil {
		log.Printf("ERROR: Failed to check report authorization for customer %s from %s: %v", customerID, c.ClientIP(), err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "failed to check report authorization"})
		return
	}

	response := models.ReportAuthorizationResponse{
		Authorizations: authorizations,
	}

	log.Printf("INFO: Successfully checked report authorization for customer %s with %d reports from %s", customerID, len(req.ReportSeqs), c.ClientIP())
	c.JSON(http.StatusOK, response)
}
