// Dev/test client for dev/test/troubleshooting.
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"

	"cleanapp/be"
)

const (
	serviceUrl  = "http://127.0.0.1:8080"
	referralUrl = "http://127.0.0.1:8081"
	contentType = "application/json"
)

var (
	testReport   = flag.Bool("test_report", true, "If true then run tests for report service")
	testReferral = flag.Bool("test_referral", false, "If true then run tests for referral service")
	userID       = fmt.Sprintf("%X", rand.Uint64())
)

func doUser() {
	log.Println("DoUser()")
	buf := `
{
	"version": "2.0",
	"id": "` + userID + `",
	"avatar": "La Puch da Vinchi"
}`

	resp, err := http.Post(serviceUrl+be.EndPointUser, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

// TODO: consider moving to common.
func RandomizeFloat(v, max float64) string {
	return fmt.Sprintf("%f", v+rand.Float64()*2*max-max)
}

func randomizeInt(v int64, max int64) int64 {
	return int64(v + int64(rand.Float64()*float64(max)*2-float64(max)))
}

func doReport() {
	log.Println("doReport()")
	buf := `
{
	"version": "2.0",
	"id": "` + userID + `",
	"latitude": ` + RandomizeFloat(35.1293548, 1.0) + `,
	"longitude": ` + RandomizeFloat(-90.1222609, 1.0) + `,
	"x": 100,
	"y": 150,
	"image": "` + base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x48}) + `"
}`

	resp, err := http.Post(serviceUrl+be.EndPointReport, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func doMap() {
	log.Println("doMap()")
	buf := `
{
	"version": "2.0",
	"id": "` + userID + `",
	"vport": {
		"lattop": 34.0,
		"lonleft": -95.0,
		"latbottom": 36.0,
		"lonright": -85.0
	}
}`

	resp, err := http.Post(serviceUrl+be.EndPointGetMap, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func doWriteReferral() {
	log.Println("doWriteReferral()")
	buf := `
	{
		"refkey": "192.168.1.34:300:670",
		"refvalue": "abcdef"
	}`
	resp, err := http.Post(referralUrl+"/writereferral", contentType, bytes.NewBufferString(buf))
	if err != nil {
		log.Printf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func doReadReferral() {
	log.Println("doReadReferral()")
	resp, err := http.Get(referralUrl + "/readreferral?refkey=" + url.QueryEscape("192.168.1.34:300:670"))
	if err != nil {
		log.Printf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func doStats() {
	log.Println("doStats()")
	buf := `
{
	"version": "2.0",
	"id": "` + userID + `"
}`

	resp, err := http.Post(serviceUrl+be.EndPointGetStats, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func main() {
	flag.Parse()

	if *testReport {
		doUser()
		doReport()
		doMap()
		doStats()
	}
	if *testReferral {
		doWriteReferral()
		doReadReferral()
	}
}
