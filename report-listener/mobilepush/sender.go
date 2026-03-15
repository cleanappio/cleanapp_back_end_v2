package mobilepush

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cleanapp-common/httpx"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Config struct {
	Enabled            bool
	APNSTeamID         string
	APNSKeyID          string
	APNSBundleID       string
	APNSAuthKeyP8      string
	APNSAuthKeyP8Path  string
	APNSUseProduction  bool
	FCMProjectID       string
	FCMCredentialsJSON string
	FCMCredentialsFile string
}

type Message struct {
	Title string
	Body  string
	Data  map[string]string
}

type Result struct {
	Provider      string
	StatusCode    int
	ResponseBody  string
	Disabled      bool
	InvalidDevice bool
}

type Sender struct {
	cfg    Config
	client *http.Client

	mu                    sync.Mutex
	apnsCachedBearerToken string
	apnsTokenIssued       time.Time
	fcmTokenSource        oauth2.TokenSource
}

func NewSender(cfg Config) *Sender {
	return &Sender{
		cfg:    cfg,
		client: httpx.NewClient(15 * time.Second),
	}
}

func (s *Sender) Send(ctx context.Context, provider, token string, message Message) (Result, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "apns":
		return s.sendAPNS(ctx, token, message)
	case "fcm":
		return s.sendFCM(ctx, token, message)
	default:
		return Result{Provider: provider, Disabled: true, ResponseBody: "unsupported provider"}, nil
	}
}

func (s *Sender) sendAPNS(ctx context.Context, deviceToken string, message Message) (Result, error) {
	result := Result{Provider: "apns"}
	if !s.cfg.Enabled || s.cfg.APNSTeamID == "" || s.cfg.APNSKeyID == "" || s.cfg.APNSBundleID == "" || (strings.TrimSpace(s.cfg.APNSAuthKeyP8) == "" && strings.TrimSpace(s.cfg.APNSAuthKeyP8Path) == "") {
		result.Disabled = true
		result.ResponseBody = "apns unconfigured"
		return result, nil
	}

	bearerToken, err := s.apnsBearerToken()
	if err != nil {
		return result, err
	}

	host := "https://api.push.apple.com"
	if !s.cfg.APNSUseProduction {
		host = "https://api.sandbox.push.apple.com"
	}
	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{
				"title": message.Title,
				"body":  message.Body,
			},
			"sound": "default",
		},
		"cleanapp": message.Data,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return result, fmt.Errorf("failed to marshal apns payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+strings.TrimSpace(deviceToken), bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("failed to create apns request: %w", err)
	}
	req.Header.Set("authorization", "bearer "+bearerToken)
	req.Header.Set("apns-topic", s.cfg.APNSBundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("content-type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return result, fmt.Errorf("apns request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	result.StatusCode = resp.StatusCode
	result.ResponseBody = strings.TrimSpace(string(respBody))
	if resp.StatusCode == http.StatusOK {
		return result, nil
	}
	if strings.Contains(result.ResponseBody, "BadDeviceToken") || strings.Contains(result.ResponseBody, "Unregistered") || strings.Contains(result.ResponseBody, "DeviceTokenNotForTopic") {
		result.InvalidDevice = true
	}
	return result, fmt.Errorf("apns push rejected with status %d", resp.StatusCode)
}

func (s *Sender) sendFCM(ctx context.Context, deviceToken string, message Message) (Result, error) {
	result := Result{Provider: "fcm"}
	if !s.cfg.Enabled || s.cfg.FCMProjectID == "" || (strings.TrimSpace(s.cfg.FCMCredentialsJSON) == "" && strings.TrimSpace(s.cfg.FCMCredentialsFile) == "") {
		result.Disabled = true
		result.ResponseBody = "fcm unconfigured"
		return result, nil
	}

	accessToken, err := s.fcmAccessToken(ctx)
	if err != nil {
		return result, err
	}

	payload := map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"notification": map[string]string{
				"title": message.Title,
				"body":  message.Body,
			},
			"data": message.Data,
			"android": map[string]any{
				"priority": "HIGH",
				"notification": map[string]string{
					"channel_id": "cleanapp_report_delivery",
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return result, fmt.Errorf("failed to marshal fcm payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fcm.googleapis.com/v1/projects/"+s.cfg.FCMProjectID+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("failed to create fcm request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return result, fmt.Errorf("fcm request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	result.StatusCode = resp.StatusCode
	result.ResponseBody = strings.TrimSpace(string(respBody))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return result, nil
	}
	if strings.Contains(result.ResponseBody, "UNREGISTERED") || strings.Contains(result.ResponseBody, "registration-token-not-registered") {
		result.InvalidDevice = true
	}
	return result, fmt.Errorf("fcm push rejected with status %d", resp.StatusCode)
}

func (s *Sender) apnsBearerToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.apnsCachedBearerToken != "" && time.Since(s.apnsTokenIssued) < 45*time.Minute {
		return s.apnsCachedBearerToken, nil
	}

	privateKeyPEM, err := s.loadAPNSKey()
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode apns auth key")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse apns auth key: %w", err)
	}
	privateKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("apns auth key is not an ecdsa private key")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": s.cfg.APNSTeamID,
		"iat": time.Now().Unix(),
	})
	token.Header["kid"] = s.cfg.APNSKeyID
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign apns provider token: %w", err)
	}
	s.apnsCachedBearerToken = signedToken
	s.apnsTokenIssued = time.Now()
	return signedToken, nil
}

func (s *Sender) loadAPNSKey() (string, error) {
	if strings.TrimSpace(s.cfg.APNSAuthKeyP8) != "" {
		return s.cfg.APNSAuthKeyP8, nil
	}
	if strings.TrimSpace(s.cfg.APNSAuthKeyP8Path) == "" {
		return "", fmt.Errorf("apns auth key is not configured")
	}
	keyBytes, err := os.ReadFile(strings.TrimSpace(s.cfg.APNSAuthKeyP8Path))
	if err != nil {
		return "", fmt.Errorf("failed to read apns auth key: %w", err)
	}
	return string(keyBytes), nil
}

func (s *Sender) fcmAccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.fcmTokenSource != nil {
		source := s.fcmTokenSource
		s.mu.Unlock()
		token, err := source.Token()
		if err != nil {
			return "", fmt.Errorf("failed to fetch fcm access token: %w", err)
		}
		return token.AccessToken, nil
	}
	s.mu.Unlock()

	credsBytes, err := s.loadFCMCredentials()
	if err != nil {
		return "", err
	}
	creds, err := google.CredentialsFromJSON(ctx, credsBytes, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return "", fmt.Errorf("failed to parse fcm credentials: %w", err)
	}

	s.mu.Lock()
	if s.fcmTokenSource == nil {
		s.fcmTokenSource = creds.TokenSource
	}
	source := s.fcmTokenSource
	s.mu.Unlock()

	token, err := source.Token()
	if err != nil {
		return "", fmt.Errorf("failed to fetch fcm access token: %w", err)
	}
	return token.AccessToken, nil
}

func (s *Sender) loadFCMCredentials() ([]byte, error) {
	if strings.TrimSpace(s.cfg.FCMCredentialsJSON) != "" {
		return []byte(s.cfg.FCMCredentialsJSON), nil
	}
	if strings.TrimSpace(s.cfg.FCMCredentialsFile) == "" {
		return nil, fmt.Errorf("fcm credentials are not configured")
	}
	creds, err := os.ReadFile(strings.TrimSpace(s.cfg.FCMCredentialsFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read fcm credentials: %w", err)
	}
	return creds, nil
}
