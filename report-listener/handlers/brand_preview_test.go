package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"report-listener/database"
	"report-listener/models"

	"github.com/gin-gonic/gin"
)

func previewRequest(query string, load func(context.Context, database.EvidenceFilter) (models.PublicBrandPreview, error)) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v3/public/discovery/brand-preview?"+query, nil)
	servePublicBrandPreview(c, load)
	return response
}

func TestBrandPreviewDefaultsLimitsAndEmptyFirsthandPresence(t *testing.T) {
	for _, query := range []string{"brand_name=rarebrand", "brand_name=rarebrand&n=999&lang=en&evidence_kind=firsthand"} {
		response := previewRequest(query, func(ctx context.Context, filter database.EvidenceFilter) (models.PublicBrandPreview, error) {
			if filter.BrandName != "rarebrand" || filter.Kind != "firsthand" || filter.Limit != 6 || filter.Language != "en" {
				t.Fatalf("incorrect public preview scope: %+v", filter)
			}
			return models.PublicBrandPreview{BrandFound: true, BrandName: "rarebrand", EvidenceKind: "firsthand", Reports: []models.PublicBrandPreviewReport{}}, nil
		})
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"reports":[]`) || response.Header().Get("Cache-Control") != "private, no-store, max-age=0" || response.Header().Get("X-Robots-Tag") != "noindex" {
			t.Fatalf("empty firsthand was hidden or cached: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestBrandPreviewRejectsInvalidInputBeforeDatabase(t *testing.T) {
	for _, query := range []string{"", "brand_name=rarebrand&n=0", "brand_name=rarebrand&n=-1", "brand_name=rarebrand&n=oops", "brand_name=rarebrand&evidence_kind=other", "brand_name=rarebrand&lang=english", "brand_name=rare%00brand", "brand_name=" + strings.Repeat("x", 256)} {
		response := previewRequest(query, func(context.Context, database.EvidenceFilter) (models.PublicBrandPreview, error) {
			t.Fatal("invalid input reached database")
			return models.PublicBrandPreview{}, nil
		})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("bad preview input accepted: %d", response.Code)
		}
	}
}

func TestBrandPreviewMissingAndUnavailableStayDistinct(t *testing.T) {
	missing := previewRequest("brand_name=rarebrand", func(context.Context, database.EvidenceFilter) (models.PublicBrandPreview, error) {
		return models.PublicBrandPreview{}, nil
	})
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), `"brand_found":false`) {
		t.Fatal("unknown brand was made indexable")
	}
	unavailable := previewRequest("brand_name=rarebrand", func(context.Context, database.EvidenceFilter) (models.PublicBrandPreview, error) {
		return models.PublicBrandPreview{}, errors.New("private database diagnostic")
	})
	if unavailable.Code != http.StatusServiceUnavailable || strings.Contains(unavailable.Body.String(), "private database diagnostic") || unavailable.Header().Get("Cache-Control") != "private, no-store, max-age=0" || unavailable.Header().Get("Retry-After") != "60" {
		t.Fatal("temporary failure was hidden, exposed, or cached")
	}
}
