package main

import (
	"auth-service/config"
	"cleanapp-common/mailtransport"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestWorkspaceRouterStartsWithoutSendGridCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		EmailProvider: "google_workspace", EmailFromName: "CleanApp", EmailFromAddress: "info@cleanapp.io",
		SMTPConfig:   mailtransport.Config{Host: "smtp-relay.gmail.com", Port: 587, Timeout: 30 * time.Second},
		RateLimitRPS: 10, RateLimitBurst: 20,
	}
	router, err := setupRouter(nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected healthy router, got %d", response.Code)
	}
}

func TestWorkspaceRouterFailsBeforeServingWhenSMTPConfigIsInvalid(t *testing.T) {
	router, err := setupRouter(nil, &config.Config{EmailProvider: "google_workspace"})
	if err == nil || router != nil {
		t.Fatalf("expected router startup failure, got router=%v error=%v", router, err)
	}
}
