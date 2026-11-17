package inbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"golang.org/x/oauth2/google"
)

// gmailReadOnlyScope is the only scope the worker asks for. It cannot send,
// label, mark read or delete.
const gmailReadOnlyScope = "https://www.googleapis.com/auth/gmail.readonly"

const (
	gmailBaseURL = "https://gmail.googleapis.com/gmail/v1/users/me"
	// gmailMaxMessagesPerPoll bounds one poll. Anything beyond it is picked up
	// by the next poll, because handled messages are skipped.
	gmailMaxMessagesPerPoll = 500
)

// Gmail reads one Google Workspace mailbox with a service account that has
// domain-wide delegation for the read-only scope. Setup steps are in
// docs/architecture/notification-outcomes.md, section 10.
type Gmail struct {
	client  *http.Client
	baseURL string
}

// NewGmail builds a reader for the mailbox of user, from a service-account
// JSON key file.
func NewGmail(ctx context.Context, credentialsFile, user string) (*Gmail, error) {
	key, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("read inbox credentials: %w", err)
	}
	config, err := google.JWTConfigFromJSON(key, gmailReadOnlyScope)
	if err != nil {
		return nil, fmt.Errorf("parse inbox credentials: %w", err)
	}
	config.Subject = user // act as this one mailbox and no other
	client := config.Client(ctx)
	client.Timeout = 30 * time.Second
	return &Gmail{client: client, baseURL: gmailBaseURL}, nil
}

func (g *Gmail) get(ctx context.Context, path string, query url.Values, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	response, err := g.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		// The body can quote the request; the status is enough to act on.
		io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		return fmt.Errorf("gmail returned HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(into)
}

// List returns message IDs received since the given time, excluding mail the
// mailbox itself sent.
func (g *Gmail) List(ctx context.Context, since time.Time) ([]string, error) {
	query := url.Values{
		"q":          {"-in:sent -in:drafts -in:chats after:" + strconv.FormatInt(since.Unix(), 10)},
		"maxResults": {"100"},
	}
	var ids []string
	for len(ids) < gmailMaxMessagesPerPoll {
		var page struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := g.get(ctx, "/messages", query, &page); err != nil {
			return nil, err
		}
		for _, message := range page.Messages {
			ids = append(ids, message.ID)
		}
		if page.NextPageToken == "" {
			break
		}
		query.Set("pageToken", page.NextPageToken)
	}
	return ids, nil
}

// Raw returns one message in RFC 5322 form and its receive time.
func (g *Gmail) Raw(ctx context.Context, id string) ([]byte, time.Time, error) {
	var message struct {
		Raw          string `json:"raw"`
		InternalDate string `json:"internalDate"`
	}
	if err := g.get(ctx, "/messages/"+url.PathEscape(id), url.Values{"format": {"raw"}}, &message); err != nil {
		return nil, time.Time{}, err
	}
	raw, err := base64.URLEncoding.DecodeString(message.Raw)
	if err != nil {
		if raw, err = base64.RawURLEncoding.DecodeString(message.Raw); err != nil {
			return nil, time.Time{}, fmt.Errorf("decode gmail message: %w", err)
		}
	}
	receivedAt := time.Now().UTC()
	if millis, err := strconv.ParseInt(message.InternalDate, 10, 64); err == nil && millis > 0 {
		receivedAt = time.UnixMilli(millis).UTC()
	}
	return raw, receivedAt, nil
}
