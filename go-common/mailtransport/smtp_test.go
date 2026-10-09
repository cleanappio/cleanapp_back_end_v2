package mailtransport

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func clearSMTPEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_PASSWORD_FILE", "SMTP_TIMEOUT"} {
		t.Setenv(key, "")
	}
}

func TestLoadConfig(t *testing.T) {
	clearSMTPEnv(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "smtp-relay.gmail.com" || cfg.Port != 587 || cfg.Timeout != 30*time.Second || cfg.Username != "" || cfg.Password != "" {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	secretPath := filepath.Join(t.TempDir(), "smtp-password")
	if err := os.WriteFile(secretPath, []byte("test-password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMTP_USERNAME", "info@cleanapp.io")
	t.Setenv("SMTP_PASSWORD_FILE", secretPath)
	cfg, err = LoadConfig()
	if err != nil || cfg.Password != "test-password" {
		t.Fatalf("mounted secret: config=%#v, err=%v", cfg, err)
	}
	t.Setenv("SMTP_PASSWORD", "other-password")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("accepted ambiguous password sources")
	}
}

func TestLoadConfigRejectsInvalidSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"SMTP_PORT", "not-a-port"},
		{"SMTP_PORT", "0"},
		{"SMTP_PORT", "65536"},
		{"SMTP_TIMEOUT", "bad-duration"},
		{"SMTP_TIMEOUT", "0s"},
		{"SMTP_HOST", "bad\r\nhost"},
		{"SMTP_USERNAME", "info@cleanapp.io"},
		{"SMTP_PASSWORD", "password-without-user"},
		{"SMTP_PASSWORD_FILE", "/nonexistent/smtp-password"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			clearSMTPEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
}

type peerOptions struct {
	noTLS         bool
	reject        string
	quitFails     bool
	stallGreeting bool
	helloFails    bool
}

type peerResult struct {
	commands []string
	data     []byte
	auth     string
	err      error
}

func startSMTPPeer(t *testing.T, options peerOptions) (Config, *x509.CertPool, <-chan peerResult) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "SMTP test CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        true, BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	results := make(chan peerResult, 1)
	go func() {
		var result peerResult
		defer func() { results <- result }()
		connection, err := listener.Accept()
		if err != nil {
			result.err = err
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(connection)
		writer := bufio.NewWriter(connection)
		send := func(value string) error {
			if _, err := writer.WriteString(value + "\r\n"); err != nil {
				return err
			}
			return writer.Flush()
		}
		if options.stallGreeting {
			_, result.err = reader.ReadByte()
			return
		}
		if err := send("220 smtp.test ESMTP"); err != nil {
			result.err = err
			return
		}
		secured := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				// A caller closing a failed session is expected in failure tests.
				return
			}
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			result.commands = append(result.commands, line)
			command := strings.SplitN(line, " ", 2)[0]
			if options.reject == command {
				_ = send("550 simulated " + command + " rejection")
				continue
			}
			if options.helloFails && (command == "EHLO" || command == "HELO") {
				_ = send("550 simulated greeting rejection")
				continue
			}
			switch command {
			case "EHLO":
				if line != "EHLO cleanapp.io" {
					result.err = fmt.Errorf("expected sender domain in EHLO, got %q", line)
					return
				}
				if !secured && !options.noTLS {
					err = send("250-smtp.test\r\n250 STARTTLS")
				} else if secured {
					err = send("250-smtp.test\r\n250 AUTH PLAIN")
				} else {
					err = send("250 smtp.test")
				}
			case "STARTTLS":
				if err = send("220 Begin TLS"); err != nil {
					break
				}
				secure := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
				if err = secure.Handshake(); err != nil {
					break
				}
				connection = secure
				reader = bufio.NewReader(secure)
				writer = bufio.NewWriter(secure)
				secured = true
			case "AUTH":
				if !secured {
					result.err = errors.New("AUTH attempted before TLS")
					return
				}
				parts := strings.SplitN(line, " ", 3)
				if len(parts) != 3 || parts[1] != "PLAIN" {
					result.err = errors.New("invalid AUTH command")
					return
				}
				decoded, decodeErr := base64.StdEncoding.DecodeString(parts[2])
				if decodeErr != nil {
					result.err = decodeErr
					return
				}
				result.auth = string(decoded)
				err = send("235 Authenticated")
			case "MAIL", "RCPT":
				err = send("250 OK")
			case "DATA":
				if err = send("354 End with dot"); err != nil {
					break
				}
				result.data, err = textproto.NewReader(reader).ReadDotBytes()
				if err != nil {
					break
				}
				if options.reject == "ACCEPT" {
					err = send("554 Message rejected")
				} else {
					err = send("250 Message accepted")
				}
			case "QUIT":
				if !options.quitFails {
					_ = send("221 Goodbye")
				}
				return
			case "*":
				err = send("501 Authentication canceled")
			default:
				result.err = fmt.Errorf("unexpected command: %s", command)
				return
			}
			if err != nil {
				result.err = err
				return
			}
		}
	}()
	return Config{Host: "127.0.0.1", Port: port, Timeout: 2 * time.Second}, pool, results
}

func basicMessage() Message {
	return Message{FromName: "CleanApp", FromEmail: "info@cleanapp.io", To: []string{"recipient@example.com"}, Subject: "Test message", Text: "First line\n.dot-stuffed line"}
}

func trustedSender(t *testing.T, cfg Config, roots *x509.CertPool) *Sender {
	t.Helper()
	sender, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sender.tlsConfig.RootCAs = roots
	return sender
}

func TestSendVerifiedSTARTTLSAndAuth(t *testing.T) {
	cfg, roots, results := startSMTPPeer(t, peerOptions{})
	cfg.Username, cfg.Password = "info@cleanapp.io", "test-password"
	sender := trustedSender(t, cfg, roots)
	id, err := sender.Send(basicMessage())
	if err != nil {
		t.Fatal(err)
	}
	result := <-results
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.auth != "\x00info@cleanapp.io\x00test-password" {
		t.Fatal("incorrect authentication payload")
	}
	helloCount := 0
	for _, command := range result.commands {
		if strings.HasPrefix(command, "EHLO ") {
			if command != "EHLO cleanapp.io" {
				t.Fatal("EHLO did not identify sender domain")
			}
			helloCount++
		}
	}
	if helloCount != 2 {
		t.Fatalf("expected domain EHLO before and after TLS, got %d", helloCount)
	}
	if sender.Provider() != "google_workspace" || Provider() != sender.Provider() {
		t.Fatal("incorrect provider")
	}
	message, err := mail.ReadMessage(bytes.NewReader(result.data))
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || message.Header.Get("Message-ID") != id {
		t.Fatal("returned ID does not identify accepted message")
	}
	if !strings.Contains(strings.Join(result.commands, "\n"), "MAIL FROM:<info@cleanapp.io>") {
		t.Fatal("envelope sender changed")
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil || !strings.Contains(string(decoded), ".dot-stuffed line") {
		t.Fatal("SMTP dot transparency changed body")
	}
}

func TestSendPropagatesGreetingFailure(t *testing.T) {
	cfg, roots, results := startSMTPPeer(t, peerOptions{helloFails: true})
	_, err := trustedSender(t, cfg, roots).Send(basicMessage())
	if err == nil || !strings.Contains(err.Error(), "SMTP EHLO:") || !strings.Contains(err.Error(), "550") {
		t.Fatalf("expected explicit greeting failure, got %v", err)
	}
	result := <-results
	if result.err != nil {
		t.Fatal(result.err)
	}
	for _, command := range result.commands {
		if command == "STARTTLS" || strings.HasPrefix(command, "MAIL ") {
			t.Fatal("continued delivery after failed greeting")
		}
	}
}

func TestSendFailsClosedWithoutTLSOrTrust(t *testing.T) {
	for _, noTLS := range []bool{true, false} {
		t.Run(fmt.Sprintf("noTLS=%v", noTLS), func(t *testing.T) {
			cfg, _, results := startSMTPPeer(t, peerOptions{noTLS: noTLS})
			cfg.Username, cfg.Password = "info@cleanapp.io", "test-password"
			sender, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if id, err := sender.Send(basicMessage()); err == nil || id != "" {
				t.Fatal("accepted insecure SMTP delivery")
			}
			result := <-results
			for _, command := range result.commands {
				if strings.HasPrefix(command, "AUTH ") || strings.HasPrefix(command, "MAIL ") {
					t.Fatal("authenticated or sent message without verified TLS")
				}
			}
		})
	}
}

func TestSendPropagatesSMTPFailures(t *testing.T) {
	for _, command := range []string{"STARTTLS", "AUTH", "MAIL", "RCPT", "DATA", "ACCEPT"} {
		t.Run(command, func(t *testing.T) {
			cfg, roots, results := startSMTPPeer(t, peerOptions{reject: command})
			cfg.Username, cfg.Password = "info@cleanapp.io", "test-password"
			sender := trustedSender(t, cfg, roots)
			id, err := sender.Send(basicMessage())
			if err == nil || id != "" {
				t.Fatal("SMTP failure reported as acceptance")
			}
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			count := 0
			for _, value := range result.commands {
				if strings.HasPrefix(value, "MAIL ") {
					count++
				}
			}
			if count > 1 {
				t.Fatal("sender retried failed delivery")
			}
		})
	}
}

func TestQuitFailureDoesNotInvalidateAcceptedMessage(t *testing.T) {
	cfg, roots, results := startSMTPPeer(t, peerOptions{quitFails: true})
	id, err := trustedSender(t, cfg, roots).Send(basicMessage())
	if err != nil || id == "" {
		t.Fatalf("accepted message treated as failure after QUIT: %v", err)
	}
	if result := <-results; result.err != nil || len(result.data) == 0 {
		t.Fatalf("message was not accepted: %v", result.err)
	}
}

func TestSendTimeout(t *testing.T) {
	cfg, roots, results := startSMTPPeer(t, peerOptions{stallGreeting: true})
	cfg.Timeout = 50 * time.Millisecond
	started := time.Now()
	_, err := trustedSender(t, cfg, roots).Send(basicMessage())
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected network timeout, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("configured timeout did not bound greeting")
	}
	<-results
}

type decodedPart struct {
	contentType string
	filename    string
	cid         string
	disposition string
	data        []byte
}

func decodeParts(t *testing.T, headers textproto.MIMEHeader, body io.Reader) []decodedPart {
	t.Helper()
	contentType, params, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(contentType, "multipart/") {
		reader := multipart.NewReader(body, params["boundary"])
		var out []decodedPart
		for {
			part, err := reader.NextRawPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, decodeParts(t, part.Header, part)...)
		}
		return out
	}
	switch headers.Get("Content-Transfer-Encoding") {
	case "quoted-printable":
		body = quotedprintable.NewReader(body)
	case "base64":
		body = base64.NewDecoder(base64.StdEncoding, body)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	disposition := ""
	filename := ""
	if value := headers.Get("Content-Disposition"); value != "" {
		var params map[string]string
		disposition, params, err = mime.ParseMediaType(value)
		if err != nil {
			t.Fatal(err)
		}
		filename = params["filename"]
	}
	return []decodedPart{{contentType: contentType, filename: filename, cid: headers.Get("Content-ID"), disposition: disposition, data: data}}
}

func TestMIMERoundTrip(t *testing.T) {
	message := basicMessage()
	message.FromName = "CleanApp Zürich"
	message.To = append(message.To, "second@example.com")
	message.Subject = "Rapport — café ☕"
	message.Text = "Bonjour café!\nPlain text."
	message.HTML = "<p>Bonjour café! <img src=\"cid:logo\"></p>"
	message.Attachments = []Attachment{
		{Filename: "logo.png", ContentType: "image/png", ContentID: "logo", Inline: true, Data: []byte{0, 1, 2, 3, 255}},
		{Filename: "rapport résumé.pdf", ContentType: "application/pdf", Data: []byte("%PDF-test")},
	}
	wire, id, from, recipients, err := encodeMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	if from != message.FromEmail || len(recipients) != len(message.To) {
		t.Fatal("envelope addresses lost")
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(wire))
	if err != nil {
		t.Fatal(err)
	}
	name, err := mail.ParseAddress(parsed.Header.Get("From"))
	if err != nil || name.Name != message.FromName || name.Address != message.FromEmail {
		t.Fatal("unicode sender name changed")
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != message.Subject || parsed.Header.Get("Message-ID") != id {
		t.Fatal("message headers changed")
	}
	parts := decodeParts(t, textproto.MIMEHeader(parsed.Header), parsed.Body)
	if len(parts) != 4 {
		t.Fatalf("expected text, HTML, inline image and attachment; got %d parts", len(parts))
	}
	if parts[0].contentType != "text/plain" || strings.ReplaceAll(string(parts[0].data), "\r\n", "\n") != message.Text || parts[1].contentType != "text/html" || string(parts[1].data) != message.HTML {
		t.Fatal("text alternatives changed")
	}
	if parts[2].cid != "<logo>" || parts[2].disposition != "inline" || !bytes.Equal(parts[2].data, message.Attachments[0].Data) {
		t.Fatal("inline image metadata or bytes changed")
	}
	if parts[3].filename != message.Attachments[1].Filename || parts[3].disposition != "attachment" || !bytes.Equal(parts[3].data, message.Attachments[1].Data) {
		t.Fatal("attachment filename or bytes changed")
	}
}

func TestMessageValidation(t *testing.T) {
	for name, mutate := range map[string]func(*Message){
		"sender injection":              func(m *Message) { m.FromEmail = "info@cleanapp.io\r\nBcc: victim@example.com" },
		"sender display address":        func(m *Message) { m.FromEmail = "Name <info@cleanapp.io>" },
		"name injection":                func(m *Message) { m.FromName = "CleanApp\r\nBcc: victim@example.com" },
		"subject injection":             func(m *Message) { m.Subject = "Subject\r\nBcc: victim@example.com" },
		"recipient injection":           func(m *Message) { m.To = []string{"recipient@example.com\r\nDATA"} },
		"no recipients":                 func(m *Message) { m.To = nil },
		"SMTPUTF8 recipient":            func(m *Message) { m.To = []string{"café@example.com"} },
		"attachment filename injection": func(m *Message) { m.Attachments = []Attachment{{Filename: "report\r\nBcc:x", Data: []byte("x")}} },
		"attachment type injection": func(m *Message) {
			m.Attachments = []Attachment{{Filename: "report", ContentType: "text/plain\r\nBcc:x"}}
		},
		"attachment CID injection": func(m *Message) {
			m.Attachments = []Attachment{{Filename: "logo", Inline: true, ContentID: "cid\r\nBcc:x"}}
		},
		"inline without CID": func(m *Message) { m.Attachments = []Attachment{{Filename: "logo", Inline: true}} },
	} {
		t.Run(name, func(t *testing.T) {
			message := basicMessage()
			mutate(&message)
			if _, _, _, _, err := encodeMessage(message); err == nil {
				t.Fatal("invalid message accepted")
			}
		})
	}
}
