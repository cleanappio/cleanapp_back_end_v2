// Dev/test client for dev/test/troubleshooting.
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"

	"cleanapp/api"

	"github.com/apex/log"
)

const (
	serviceUrl  = "http://127.0.0.1:8080"
	contentType = "application/json"
)

var (
	userID = fmt.Sprintf("%X", rand.Uint64())
)

func doUser() {
	log.Info("DoUser()")
	buf := `
{
	"version": "2.0",
	"id": "` + userID + `",
	"avatar": "La Puch da Vinchi"
}`

	resp, err := http.Post(serviceUrl+api.UserEndpoint, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Errorf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	log.Infof("Done, %s: %v", resp.Status, string(body))
}

// TODO: consider moving to common.
func RandomizeFloat(v, max float64) string {
	return fmt.Sprintf("%f", v+rand.Float64()*2*max-max)
}

func doReport() {
	log.Infof("doReport()")
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

	resp, err := http.Post(serviceUrl+api.ReportEndpoint, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Errorf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	log.Infof("Done, %s: %v", resp.Status, string(body))
}

func doMap() {
	log.Infof("doMap()")
	buf := `
	{
		"version": "2.0",
		"id": "` + userID + `",
		"vport": {
			"lattop": 34.0,
			"lonleft": -95.0,
			"latbottom": 36.0,
			"lonright": -85.0
		},
		"s2cells": []
	}`

	resp, err := http.Post(serviceUrl+api.GetMapEndpoint, contentType, bytes.NewBufferString(buf))

	if err != nil {
		log.Errorf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	log.Infof("Done, %s: %v", resp.Status, string(body))
}

func doWriteReferral() {
	log.Info("doWriteReferral()")
	buf := `
	{
		"refkey": "192.168.1.34:300:670",
		"refvalue": "abcdef"
	}`
	resp, err := http.Post(serviceUrl + api.WriteReferralEndpoint, contentType, bytes.NewBufferString(buf))
	if err != nil {
		log.Errorf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	log.Infof("Done, %s: %v", resp.Status, string(body))
}

func doReadReferral() {
	log.Info("doReadReferral()")
	resp, err := http.Get(serviceUrl + api.ReadReferralEndpoint + "?refkey=" + url.QueryEscape("192.168.1.34:300:670"))
	if err != nil {
		log.Errorf("Failed to call the server with %w", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	log.Infof("Done, %s: %v", resp.Status, string(body))
}

func main() {
	flag.Parse()

	doUser()
	doReport()
	doMap()
	doWriteReferral()
	doReadReferral()
}
