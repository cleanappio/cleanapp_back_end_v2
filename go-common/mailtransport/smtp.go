// Package mailtransport sends application email through Google Workspace SMTP.
// Every connection requires verified STARTTLS. Delivery is never retried here:
// an interrupted SMTP response can leave delivery status uncertain.
package mailtransport

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"cleanapp-common/appenv"
)

const provider = "google_workspace"

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	Timeout  time.Duration
}

type Message struct {
	FromName    string
	FromEmail   string
	To          []string
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
}

type Attachment struct {
	Filename    string
	ContentType string
	ContentID   string
	Data        []byte
	Inline      bool
}

// LoadConfig reads SMTP settings. SMTP_PASSWORD_FILE supports mounted secrets.
// Supplying both SMTP_PASSWORD and SMTP_PASSWORD_FILE is an error.
func LoadConfig() (Config, error) {
	port, err := strconv.Atoi(appenv.String("SMTP_PORT", "587"))
	if err != nil {
		return Config{}, fmt.Errorf("SMTP_PORT must be an integer")
	}
	timeout, err := time.ParseDuration(appenv.String("SMTP_TIMEOUT", "30s"))
	if err != nil {
		return Config{}, fmt.Errorf("SMTP_TIMEOUT must be a duration")
	}
	cfg := Config{
		Host:     appenv.String("SMTP_HOST", "smtp-relay.gmail.com"),
		Port:     port,
		Username: appenv.String("SMTP_USERNAME", ""),
		Timeout:  timeout,
	}
	passwordFile := appenv.String("SMTP_PASSWORD_FILE", "")
	passwordSet := appenv.String("SMTP_PASSWORD", "") != ""
	if passwordFile != "" && passwordSet {
		return Config{}, fmt.Errorf("set only one of SMTP_PASSWORD and SMTP_PASSWORD_FILE")
	}
	if passwordFile != "" {
		secret, err := os.ReadFile(passwordFile)
		if err != nil {
			return Config{}, fmt.Errorf("read SMTP_PASSWORD_FILE: %w", err)
		}
		cfg.Password = strings.TrimSpace(string(secret))
	} else if passwordSet {
		cfg.Password, err = appenv.Secret("SMTP_PASSWORD", "")
		if err != nil {
			return Config{}, err
		}
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

type Sender struct {
	config Config
	// Unexported: production callers cannot disable certificate verification.
	// Package tests can install the local peer's trusted CA here.
	tlsConfig *tls.Config
}

func Provider() string { return provider }

func (*Sender) Provider() string { return provider }

func New(cfg Config) (*Sender, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return &Sender{config: cfg, tlsConfig: &tls.Config{
		ServerName: cfg.Host,
		MinVersion: tls.VersionTLS12,
	}}, nil
}

func validateConfig(cfg Config) error {
	if cfg.Host == "" || strings.ContainsAny(cfg.Host, " \t\r\n\x00/\\") {
		return fmt.Errorf("SMTP_HOST must be a hostname or IP address")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("SMTP_PORT must be between 1 and 65535")
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("SMTP_TIMEOUT must be positive")
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must be configured together")
	}
	if strings.ContainsAny(cfg.Username, "\r\n\x00") || strings.ContainsRune(cfg.Password, '\x00') {
		return fmt.Errorf("invalid SMTP authentication settings")
	}
	return nil
}

// Send returns the generated RFC Message-ID only after the server accepts DATA.
// It does not retry any failures, including failures with uncertain delivery.
func (s *Sender) Send(message Message) (string, error) {
	wire, id, from, recipients, err := encodeMessage(message)
	if err != nil {
		return "", err
	}
	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	conn, err := net.DialTimeout("tcp", address, s.config.Timeout)
	if err != nil {
		return "", fmt.Errorf("SMTP connect: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(s.config.Timeout)); err != nil {
		return "", fmt.Errorf("SMTP deadline: %w", err)
	}
	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		return "", fmt.Errorf("SMTP greeting: %w", err)
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return "", fmt.Errorf("SMTP server must support STARTTLS")
	}
	if err := client.StartTLS(s.tlsConfig.Clone()); err != nil {
		return "", fmt.Errorf("SMTP STARTTLS: %w", err)
	}
	if s.config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return "", fmt.Errorf("SMTP authentication: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return "", fmt.Errorf("SMTP sender: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return "", fmt.Errorf("SMTP recipient: %w", err)
		}
	}
	data, err := client.Data()
	if err != nil {
		return "", fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := data.Write(wire); err != nil {
		return "", fmt.Errorf("SMTP message write: %w", err)
	}
	if err := data.Close(); err != nil {
		return "", fmt.Errorf("SMTP message acceptance: %w", err)
	}
	// A QUIT error after successful DATA must not turn acceptance into a failure
	// and encourage the caller to resend an already accepted message.
	_ = client.Quit()
	return id, nil
}
