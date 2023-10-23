// Dev/test client for dev/test/troubleshooting.
package main

import (
	"bytes"
	"cleanapp/be"
	"encoding/base64"
	//"fmt"
	"io"
	"log"
	"net/http"
)

const (
	url         = "http://127.0.0.1:8080"
	contentType = "application/json"
)

func doUser() {
	buf := `
{
	"version": "2.0",
	"id": "0123456789ABBCDEF",
	"avatar": "La Puch da Vinchi"
}`

	resp, err := http.Post(url+be.EndPointUser, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %v", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func doReport() {
	buf := `
{
	"version": "2.0",
	"id": "0123456789ABBCDEF",
	"latitude": 35.1293548,
	"longitude": -90.1222609,
	"x": 100,
	"y": 200,
	"image": "` + base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x48}) + `"
}`

	resp, err := http.Post(url+be.EndPointReport, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %v", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func main() {
	doUser()
	doReport()
}
