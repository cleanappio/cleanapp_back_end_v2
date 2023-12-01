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

	"cleanapp/be"
)

// 34.132.121.53
const (
	serviceUrl  = "http://127.0.0.1:8080" // Local
	// serviceUrl  = "http://34.132.121.53:80" // Google Cloud
	contentType = "application/json"
)

var (
	userID       = fmt.Sprintf("%X", rand.Uint64())
)

func doUser() {
	log.Println("DoUser()")
	buf := `
{
	"version": "2.0",
	"id": "` + userID + `",
	"avatar": "La Puch da Vinchi",
	"referral": "AaBbCc"
}`

	resp, err := http.Post(serviceUrl+be.EndPointUser, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %v", err)
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
	buf := fmt.Sprintf(`
{
	"version": "2.0",
	"id": "%s",
	"latitude": %s,
	"longitude": %s,
	"x": %f,
	"y": %f,
	"image": "%s"
}`, userID, RandomizeFloat(35.1293548, 1.0), RandomizeFloat(-90.1222609, 1.0), rand.Float64(), rand.Float64(),
    base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x48}))

	resp, err := http.Post(serviceUrl+be.EndPointReport, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %v", err)
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
		log.Printf("Failed to call the server with %v", err)
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
	resp, err := http.Post(serviceUrl+"/write_referral", contentType, bytes.NewBufferString(buf))
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
	buf := `
	{
		"refkey": "192.168.1.34:300:670"
	}`
	resp, err := http.Post(serviceUrl + "/read_referral", contentType, bytes.NewBufferString(buf))
	if err != nil {
		log.Printf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func doGenerateReferral() {
	log.Println("doGenerateReferral()")
	buf := `
	{
		"version": "2.0",
		"id": "` + userID + `"
	}`
	resp, err := http.Post(serviceUrl + "/generate_referral", contentType, bytes.NewBufferString(buf))
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

func doTeams() {
	log.Println("doTeams()")
	buf := `
	{
		"version": "2.0",
		"id": "` + userID + `"
	}`

	resp, err := http.Post(serviceUrl+be.EndPointGetTeams, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Printf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	log.Printf("Done, %s: %v", resp.Status, string(body))
}

func doTopScores() {
	log.Printf("doTopScores()")
	buf := `
	{
		"version": "2.0",
		"id": "` + userID + `"
	}`

	resp, err := http.Post(serviceUrl+be.EndPointGetTopScores, contentType, bytes.NewBufferString(buf))

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

		doUser()
		doReport()
		doMap()
		doStats()
		doTeams()
		doTopScores()
		doWriteReferral()
		doReadReferral()
		doGenerateReferral()
}
