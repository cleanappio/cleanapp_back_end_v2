package service

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"report-listener/config"
)

// The worker must stay off, and say why, rather than stop the service.
func TestInboxWorkerStaysOffWithoutFlagSecretOrCredentials(t *testing.T) {
	secret := strings.Repeat("s", 32)
	for name, tc := range map[string]struct {
		cfg  config.Config
		want string
	}{
		"flag off":            {config.Config{ReplyTokenSecret: secret, InboxCredentialsFile: "/x", InboxUser: "info@cleanapp.io"}, ""},
		"short secret":        {config.Config{InboxEnabled: true, ReplyTokenSecret: "short", InboxCredentialsFile: "/x", InboxUser: "info@cleanapp.io"}, "REPLY_TOKEN_SECRET"},
		"no credentials file": {config.Config{InboxEnabled: true, ReplyTokenSecret: secret, InboxUser: "info@cleanapp.io"}, "INBOX_CREDENTIALS_FILE"},
		"unreadable key":      {config.Config{InboxEnabled: true, ReplyTokenSecret: secret, InboxCredentialsFile: "/nonexistent/key.json", InboxUser: "info@cleanapp.io"}, "inbox worker is off"},
	} {
		var logged bytes.Buffer
		log.SetOutput(&logged)
		s := &Service{config: &tc.cfg}
		s.startInboxWorker(context.Background())
		log.SetOutput(log.Writer())
		s.wg.Wait() // returns at once: no goroutine may have been started
		if tc.want == "" && logged.Len() != 0 {
			t.Fatalf("%s: nothing should be logged: %s", name, logged.String())
		}
		if !strings.Contains(logged.String(), tc.want) {
			t.Fatalf("%s: log=%q want %q", name, logged.String(), tc.want)
		}
		if strings.Contains(logged.String(), secret) {
			t.Fatalf("%s: the secret must never be logged", name)
		}
	}
}
