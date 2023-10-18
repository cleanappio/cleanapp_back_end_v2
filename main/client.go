// Dev client for dev/test/troubleshooting.
package main

import (
	"bytes"
	"cleanapp/be"
	"io"
	"log"
	"net/http"
)

const (
	url         = "http://127.0.0.1:8080"
	contentType = "application/json"
)

func main() {
	buf := `
{
	"version": "2.0",
	"id": "0123456789ABBCDEF",
	"lattitude": 35.1293548,
	"longitue": -90.1222609,
	"x": 100,
	"y": 200,
	"image": null
}`

	resp, err := http.Post(url+be.EndPointReport, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %v", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %v", string(body))
}
