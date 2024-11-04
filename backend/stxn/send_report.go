package stxn

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"

	"github.com/apex/log"
	ethcommon "github.com/ethereum/go-ethereum/common"
)

var (
	solverUrl = flag.String("solver_url", "http://localhost:8888/report", "The URL to connect to the solver.")
)

type ReportRequest struct {
	Account string `json:"account"`
	Amount  string `json:"amount"`
}

func SendReport(receiver ethcommon.Address, amount *big.Int) error {
	log.Info("SendReport")
	request := &ReportRequest{
		Account: receiver.String(),
		Amount:  fmt.Sprintf("0x%x", amount),
	}
	jsonData, err := json.Marshal(request)
	if err != nil {
		log.Errorf("Error marshalling JSON: %w", err)
		return err
	}
	log.Infof("JSON created: %v", string(jsonData))

	req, err := http.NewRequest("POST", *solverUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Errorf("Error creating request:%w", err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("Error making request: %w", err)
		return err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Error reading response body: %w", err)
		return err
	}

	log.Infof("Response status: %v, body: %v", resp.Status, body)
	return nil
}
