package llm

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

func ShouldSkipImageAttachment(imageData []byte, mimeType string) bool {
	// Heuristic:
	// - For small images (especially 1x1 placeholders) validate they're decodable and not trivially tiny.
	// - For larger images, rely on DetectContentType only (keeps us from accidentally dropping valid webp, etc.).
	if len(imageData) >= 512 {
		return false
	}
	switch mimeType {
	case "image/png", "image/jpeg", "image/gif":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(imageData))
		if err != nil {
			return true
		}
		if cfg.Width <= 2 && cfg.Height <= 2 {
			return true
		}
		return false
	default:
		// Small, uncommon image type. Assume it's intentional and let the provider decide.
		return false
	}
}
