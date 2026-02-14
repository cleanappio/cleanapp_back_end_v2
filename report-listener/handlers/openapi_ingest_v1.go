package handlers

import (
	_ "embed"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed openapi/cleanapp-ingest.v1.yaml
var ingestOpenAPIV1YAML string

func (h *Handlers) ServeIngestOpenAPI(c *gin.Context) {
	c.Header("Content-Type", "application/yaml; charset=utf-8")
	c.String(http.StatusOK, ingestOpenAPIV1YAML)
}

func (h *Handlers) ServeIngestSwaggerUI(c *gin.Context) {
	// Minimal Swagger UI page loading assets from a CDN.
	// This keeps the binary small while still providing an easy-to-use docs surface.
	specURL := strings.TrimRight(c.Request.Host, "/")
	_ = specURL
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width,initial-scale=1" />
    <title>CleanApp Ingest v1 API</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
    <style>
      body { margin: 0; background: #0b1220; }
      #swagger-ui { max-width: 1200px; margin: 0 auto; }
    </style>
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.onload = function() {
        SwaggerUIBundle({
          url: '/v1/openapi.yaml',
          dom_id: '#swagger-ui',
          presets: [SwaggerUIBundle.presets.apis],
          layout: 'BaseLayout'
        });
      };
    </script>
  </body>
</html>`)
}
