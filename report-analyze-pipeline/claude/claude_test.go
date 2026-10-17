package claude

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"report-analyze-pipeline/llm"
	"report-analyze-pipeline/parser"
	"strings"
	"testing"
)

const analysis = `{"title":"Save fails","description":"The save button returns an error.","classification":"digital","user_info":{"name":null,"email":null,"company":null,"role":null,"company_size":null},"location":null,"brand_name":null,"litter_probability":0,"hazard_probability":0,"digital_bug_probabilty":1,"severity_level":0.3,"legal_risk_estimate":"Unknown","is_valid":true,"responsible_party":null,"inferred_contact_emails":[],"suggested_remediation":["Reproduce","Test","Correct data","Inform users"]}`

type trip func(*http.Request) (*http.Response, error)

func (f trip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func clientWithRequestCheck(t *testing.T, check func(map[string]any)) *Client {
	t.Helper()
	return newClient("stub-key", "", &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.anthropic.com" || r.URL.Path != "/v1/messages" || r.URL.RawQuery != "" {
			t.Fatal("paid provider fallback or credential URL")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		check(body)
		raw, _ := json.Marshal(map[string]any{"stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": analysis}}})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})})
}
func TestTextAnalysisSharesSubjectAttributionAndSchema(t *testing.T) {
	description := "On CleanApp itself, I cannot group brands such as Grok and Claude."
	c := clientWithRequestCheck(t, func(body map[string]any) {
		if body["system"] != llm.TextAnalysisPrompt || !strings.Contains(body["system"].(string), llm.BrandAttributionRules) {
			t.Fatal("subject rules drifted")
		}
		blocks := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
		if len(blocks) != 1 || blocks[0].(map[string]any)["text"] != description {
			t.Fatal("original text changed")
		}
		props := body["output_config"].(map[string]any)["format"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
		if props["digital_bug_probabilty"] == nil || props["brand_name"] == nil {
			t.Fatal("existing JSON fields changed")
		}
	})
	text, err := c.AnalyzeImage(nil, description)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseAnalysis(text)
	if err != nil || parsed.BrandName != "" || parsed.Classification != parser.ClassificationDigital {
		t.Fatalf("%+v %v", parsed, err)
	}
	if c.SourceName() != "Claude" {
		t.Fatal("wrong persisted source")
	}
}
func TestOriginalImageFormatsAndLocationPromptArePreserved(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			var b bytes.Buffer
			img := image.NewRGBA(image.Rect(0, 0, 12, 8))
			switch format {
			case "png":
				png.Encode(&b, img)
			case "jpeg":
				jpeg.Encode(&b, img, nil)
			case "gif":
				gif.Encode(&b, img, nil)
			}
			location := &llm.LocationContext{PrimaryName: "Station", Domain: "example.org", Country: "Switzerland"}
			c := clientWithRequestCheck(t, func(body map[string]any) {
				if body["system"] != llm.ImageAnalysisPrompt+llm.LocationPrompt(location) {
					t.Fatal("location/subject prompt drift")
				}
				blocks := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
				if len(blocks) != 2 {
					t.Fatal("image omitted")
				}
				source := blocks[1].(map[string]any)["source"].(map[string]any)
				raw, err := base64.StdEncoding.DecodeString(source["data"].(string))
				if err != nil || !bytes.Equal(raw, b.Bytes()) || source["media_type"] != "image/"+format {
					t.Fatal("original image bytes/format changed")
				}
			})
			if _, err := c.AnalyzeImageWithLocation(b.Bytes(), "Original report", location); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestPlaceholderAndTranslationUseExistingContracts(t *testing.T) {
	var tiny bytes.Buffer
	png.Encode(&tiny, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	c := clientWithRequestCheck(t, func(body map[string]any) {
		if body["system"] != llm.TextAnalysisPrompt {
			t.Fatal("omitted image still used vision prompt")
		}
		if len(body["messages"].([]any)[0].(map[string]any)["content"].([]any)) != 1 {
			t.Fatal("placeholder sent")
		}
	})
	if _, err := c.AnalyzeImage(tiny.Bytes(), "text"); err != nil {
		t.Fatal(err)
	}
	c = clientWithRequestCheck(t, func(body map[string]any) {
		text := body["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
		if text != "Please translate values in the following JSON to German. Translate all values except the field classification.\n\n"+analysis {
			t.Fatal("translation prompt changed")
		}
	})
	if _, err := c.TranslateAnalysis(analysis, "German"); err != nil {
		t.Fatal(err)
	}
}

func TestOmittedImageUsesTextPromptWithLocation(t *testing.T) {
	location := &llm.LocationContext{PrimaryName: "Station", Domain: "example.org"}
	for _, data := range [][]byte{nil, []byte("not an image"), []byte("\x89PNG\r\n\x1a\ninvalid")} {
		c := clientWithRequestCheck(t, func(body map[string]any) {
			if body["system"] != llm.TextAnalysisPrompt+llm.LocationPrompt(location) {
				t.Fatal("omitted image used image-only instructions")
			}
		})
		if _, err := c.AnalyzeImageWithLocation(data, "text", location); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImageOnlyReportOmitsEmptyTextBlock(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 12, 8)))
	c := clientWithRequestCheck(t, func(body map[string]any) {
		blocks := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
		if body["system"] != llm.ImageAnalysisPrompt || len(blocks) != 1 || blocks[0].(map[string]any)["type"] != "image" {
			t.Fatal("image-only report contains invalid empty text block")
		}
	})
	if _, err := c.AnalyzeImage(b.Bytes(), ""); err != nil {
		t.Fatal(err)
	}
}
