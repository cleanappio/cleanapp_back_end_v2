package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"report-listener/publicdiscovery"
)

func TestPublicMediaRejectsNonReportTokensBeforeDatabaseAccess(t *testing.T) {
	codec, err := publicdiscovery.NewCodec("media-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{discoveryCodec: codec} // no database: rejected tokens must never query it
	validID := "rpt_aUl-fseGsxzbWPAXbgKxGw"
	valid := publicdiscovery.Payload{Kind: publicdiscovery.KindReport, Classification: "digital", PublicID: validID, ExpiresAtUnix: time.Now().Add(time.Hour).Unix()}
	token, _ := codec.Seal(valid)
	if got, ok := h.publicMediaReportID(token); !ok || got != validID {
		t.Fatalf("valid report token rejected: %s %v", got, ok)
	}
	invalid := []string{"", "invalid", strings.Repeat("x", 2049)}
	for _, payload := range []publicdiscovery.Payload{
		{Kind: publicdiscovery.KindBrand, Classification: "digital", BrandName: "grok", ExpiresAtUnix: valid.ExpiresAtUnix},
		{Kind: publicdiscovery.KindReport, Classification: "private", PublicID: validID, ExpiresAtUnix: valid.ExpiresAtUnix},
		{Kind: publicdiscovery.KindReport, Classification: "digital", PublicID: "../../private", ExpiresAtUnix: valid.ExpiresAtUnix},
		{Kind: publicdiscovery.KindReport, Classification: "digital", PublicID: validID, ExpiresAtUnix: time.Now().Add(-time.Hour).Unix()},
	} {
		value, _ := codec.Seal(payload)
		invalid = append(invalid, value)
	}
	for _, token := range invalid {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v3/public/report-media?token="+url.QueryEscape(token), nil)
		h.GetPublicReportMedia(ctx)
		ctx.Writer.WriteHeaderNow()
		if response.Code != http.StatusBadRequest || response.Body.Len() != 0 || response.Header().Get("Cache-Control") != "private, no-store, max-age=0" {
			t.Fatalf("invalid token returned public media: status=%d body=%q", response.Code, response.Body.String())
		}
	}
}

func TestLegacyPublicImageResponsesCannotBeCached(t *testing.T) {
	h := &Handlers{} // Invalid input must be rejected before any database access.
	for _, serve := range []func(*gin.Context){h.GetImageByPublicID, h.GetRawImageByPublicID} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/image", nil)
		serve(ctx)
		if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "private, no-store, max-age=0" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("legacy public image denial omitted revocable-media headers")
		}
	}
}
