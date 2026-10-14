package evidence

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
)

// ValidOriginalImage rejects arbitrary bytes and tiny placeholder blobs. WebP
// remains supported by providers although the standard library has no decoder.
func ValidOriginalImage(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return false
	}
	if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		return config.Width > 1 && config.Height > 1
	}
	return mime == "image/webp" && len(data) > 512
}
