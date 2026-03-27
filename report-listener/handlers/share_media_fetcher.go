package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"log"
	"mime"
	"net/http"
	neturl "net/url"
	"path"
	"strings"
	"time"

	"report-listener/database"

	xhtml "golang.org/x/net/html"
)

const (
	digitalShareFetcherUserAgent         = "CleanAppShareFetcher/1.0 (+https://cleanapp.io)"
	digitalShareSlackbotUserAgent        = "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"
	digitalShareFacebookCrawlerUserAgent = "facebookexternalhit/1.1"
	digitalShareTwitterbotUserAgent      = "Twitterbot/1.0"
	digitalShareRemoteFetchTimeout       = 8 * time.Second
)

type digitalShareRemoteFetchOptions struct {
	MaxImages     int
	MaxTotalBytes int
}

type digitalShareRemoteDocument struct {
	FinalURL    *neturl.URL
	ContentType string
	Body        []byte
}

func FetchRemoteDigitalShareAttachments(
	ctx context.Context,
	sourceURL string,
	maxImages int,
	maxTotalBytes int,
) ([]database.DigitalShareAttachment, error) {
	attachments, err := fetchDigitalShareRemoteImages(ctx, newDigitalShareRemoteHTTPClient(), sourceURL, digitalShareRemoteFetchOptions{
		MaxImages:     maxImages,
		MaxTotalBytes: maxTotalBytes,
	})
	if err != nil {
		return nil, err
	}

	out := make([]database.DigitalShareAttachment, 0, len(attachments))
	for idx, attachment := range attachments {
		out = append(out, database.DigitalShareAttachment{
			Ordinal:  idx,
			Filename: attachment.Filename,
			MIMEType: attachment.MimeType,
			SHA256:   attachment.SHA256Hex,
			Bytes:    attachment.Bytes,
		})
	}
	return out, nil
}

func (h *Handlers) enrichDigitalSharePayloadWithRemoteImages(
	ctx context.Context,
	payload normalizedDigitalSharePayload,
) normalizedDigitalSharePayload {
	if strings.TrimSpace(payload.SourceURL) == "" || len(payload.Images) >= maxDigitalShareImageCount {
		return payload
	}

	currentBytes := 0
	seenSHA := make(map[string]struct{}, len(payload.Images))
	for _, image := range payload.Images {
		currentBytes += len(image.Bytes)
		if sha := strings.ToLower(strings.TrimSpace(image.SHA256Hex)); sha != "" {
			seenSHA[sha] = struct{}{}
		}
	}

	remainingBytes := maxDigitalShareTotalBytes - currentBytes
	if remainingBytes <= 0 {
		return payload
	}

	attachments, err := fetchDigitalShareRemoteImages(ctx, newDigitalShareRemoteHTTPClient(), payload.SourceURL, digitalShareRemoteFetchOptions{
		MaxImages:     maxDigitalShareImageCount - len(payload.Images),
		MaxTotalBytes: remainingBytes,
	})
	if err != nil {
		log.Printf("digital share: remote media fetch failed source_url=%s err=%v", payload.SourceURL, err)
		return payload
	}

	for _, attachment := range attachments {
		sha := strings.ToLower(strings.TrimSpace(attachment.SHA256Hex))
		if sha != "" {
			if _, exists := seenSHA[sha]; exists {
				continue
			}
			seenSHA[sha] = struct{}{}
		}
		payload.Images = append(payload.Images, attachment)
		payload.RemoteImageCount++
	}

	if payload.RemoteImageCount > 0 {
		payload.SharedPayloadType = classifyDigitalSharePayloadType(payload.SourceURL, payload.SharedText, len(payload.Images))
	}
	return payload
}

func fetchDigitalShareRemoteImages(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	opts digitalShareRemoteFetchOptions,
) ([]digitalShareImageAttachment, error) {
	if client == nil {
		client = newDigitalShareRemoteHTTPClient()
	}
	if opts.MaxImages <= 0 || opts.MaxTotalBytes <= 0 {
		return nil, nil
	}

	var (
		candidateURLs []string
		docErrors     []string
	)

	for _, userAgent := range digitalShareHTMLUserAgents(sourceURL) {
		doc, err := fetchDigitalShareRemoteDocument(ctx, client, sourceURL, userAgent, min(maxDigitalShareImageBytes, opts.MaxTotalBytes))
		if err != nil {
			docErrors = append(docErrors, fmt.Sprintf("%s => %v", userAgent, err))
			continue
		}

		if isSupportedDigitalShareImageContentType(doc.ContentType) {
			attachment, err := buildDigitalShareRemoteAttachment(doc.FinalURL, doc.ContentType, doc.Body)
			if err != nil {
				docErrors = append(docErrors, fmt.Sprintf("%s => %v", userAgent, err))
				continue
			}
			return []digitalShareImageAttachment{attachment}, nil
		}

		candidateURLs = extractDigitalShareImageCandidates(doc.FinalURL, string(doc.Body))
		if len(candidateURLs) > 0 {
			break
		}
	}

	if len(candidateURLs) == 0 {
		if len(docErrors) > 0 {
			return nil, fmt.Errorf("no remote share media candidates found (%s)", strings.Join(docErrors, "; "))
		}
		return nil, nil
	}

	attachments := make([]digitalShareImageAttachment, 0, min(len(candidateURLs), opts.MaxImages))
	seenSHA := make(map[string]struct{}, len(candidateURLs))
	remainingBytes := opts.MaxTotalBytes
	var downloadErrors []string
	for _, candidateURL := range candidateURLs {
		if len(attachments) >= opts.MaxImages || remainingBytes <= 0 {
			break
		}
		attachment, err := downloadDigitalShareImage(ctx, client, candidateURL, min(maxDigitalShareImageBytes, remainingBytes))
		if err != nil {
			downloadErrors = append(downloadErrors, fmt.Sprintf("%s => %v", candidateURL, err))
			continue
		}
		if _, exists := seenSHA[strings.ToLower(attachment.SHA256Hex)]; exists {
			continue
		}
		seenSHA[strings.ToLower(attachment.SHA256Hex)] = struct{}{}
		attachments = append(attachments, attachment)
		remainingBytes -= len(attachment.Bytes)
	}

	if len(attachments) > 0 {
		return attachments, nil
	}
	if len(downloadErrors) > 0 {
		return nil, fmt.Errorf("failed to download remote share media (%s)", strings.Join(downloadErrors, "; "))
	}
	return nil, nil
}

func newDigitalShareRemoteHTTPClient() *http.Client {
	return &http.Client{Timeout: digitalShareRemoteFetchTimeout}
}

func digitalShareHTMLUserAgents(sourceURL string) []string {
	agents := []string{digitalShareFetcherUserAgent}
	parsed, err := neturl.Parse(strings.TrimSpace(sourceURL))
	if err != nil {
		return appendUniqueStrings(agents, digitalShareSlackbotUserAgent)
	}

	switch strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www.")) {
	case "x.com", "twitter.com":
		return appendUniqueStrings(agents, digitalShareTwitterbotUserAgent, digitalShareSlackbotUserAgent)
	case "instagram.com":
		return appendUniqueStrings(agents, digitalShareFacebookCrawlerUserAgent, digitalShareSlackbotUserAgent)
	default:
		return appendUniqueStrings(agents, digitalShareSlackbotUserAgent)
	}
}

func fetchDigitalShareRemoteDocument(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	userAgent string,
	maxBytes int,
) (digitalShareRemoteDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return digitalShareRemoteDocument{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,image/*,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return digitalShareRemoteDocument{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return digitalShareRemoteDocument{}, fmt.Errorf("status %d: %s", resp.StatusCode, compactWhitespace(string(body)))
	}

	if maxBytes <= 0 {
		maxBytes = maxDigitalShareImageBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes+1)))
	if err != nil {
		return digitalShareRemoteDocument{}, fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxBytes {
		return digitalShareRemoteDocument{}, fmt.Errorf("response body exceeded %d bytes", maxBytes)
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if contentType == "" {
		contentType = strings.ToLower(http.DetectContentType(body))
	}
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	finalURL := resp.Request.URL
	if finalURL == nil {
		parsed, parseErr := neturl.Parse(sourceURL)
		if parseErr != nil {
			return digitalShareRemoteDocument{}, fmt.Errorf("invalid final url: %w", parseErr)
		}
		finalURL = parsed
	}

	return digitalShareRemoteDocument{
		FinalURL:    finalURL,
		ContentType: contentType,
		Body:        body,
	}, nil
}

func extractDigitalShareImageCandidates(pageURL *neturl.URL, rawHTML string) []string {
	doc, err := xhtml.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return nil
	}

	candidates := []string{}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			switch strings.ToLower(node.Data) {
			case "meta":
				key := strings.ToLower(strings.TrimSpace(firstNonEmptyShare(
					attrValue(node, "property"),
					attrValue(node, "name"),
					attrValue(node, "itemprop"),
				)))
				if digitalShareMetaImageKey(key) {
					candidates = appendResolvedDigitalShareImageCandidate(candidates, pageURL, attrValue(node, "content"))
				}
			case "link":
				if digitalShareLinkRelIncludesImage(attrValue(node, "rel")) {
					candidates = appendResolvedDigitalShareImageCandidate(candidates, pageURL, attrValue(node, "href"))
				}
			case "script":
				if strings.EqualFold(strings.TrimSpace(attrValue(node, "type")), "application/ld+json") {
					for _, candidate := range extractDigitalShareJSONLDImageCandidates(nodeText(node)) {
						candidates = appendResolvedDigitalShareImageCandidate(candidates, pageURL, candidate)
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return candidates
}

func digitalShareMetaImageKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "og:image", "og:image:url", "og:image:secure_url", "twitter:image", "twitter:image:src", "image", "thumbnailurl", "thumbnail", "itemprop:image":
		return true
	default:
		return false
	}
}

func digitalShareLinkRelIncludesImage(rel string) bool {
	for _, token := range strings.Fields(strings.ToLower(strings.TrimSpace(rel))) {
		if token == "image_src" {
			return true
		}
	}
	return false
}

func extractDigitalShareJSONLDImageCandidates(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}

	candidates := []string{}
	collectDigitalShareJSONLDImageCandidates(payload, &candidates)
	return candidates
}

func collectDigitalShareJSONLDImageCandidates(value any, candidates *[]string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectDigitalShareJSONLDImageCandidates(item, candidates)
		}
	case map[string]any:
		for key, nested := range typed {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "image":
				appendDigitalShareJSONLDImageValue(candidates, nested)
			default:
				collectDigitalShareJSONLDImageCandidates(nested, candidates)
			}
		}
	}
}

func appendDigitalShareJSONLDImageValue(candidates *[]string, value any) {
	switch typed := value.(type) {
	case string:
		*candidates = appendUniqueStrings(*candidates, strings.TrimSpace(typed))
	case []any:
		for _, item := range typed {
			appendDigitalShareJSONLDImageValue(candidates, item)
		}
	case map[string]any:
		for _, key := range []string{"url", "contentUrl", "thumbnailUrl"} {
			if raw, ok := typed[key]; ok {
				appendDigitalShareJSONLDImageValue(candidates, raw)
			}
		}
	}
}

func appendResolvedDigitalShareImageCandidate(base []string, pageURL *neturl.URL, raw string) []string {
	raw = stdhtml.UnescapeString(strings.TrimSpace(raw))
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
		return base
	}

	parsed, err := neturl.Parse(raw)
	if err != nil {
		return base
	}
	if pageURL != nil {
		parsed = pageURL.ResolveReference(parsed)
	}
	if parsed == nil {
		return base
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return base
	}
	return appendUniqueStrings(base, parsed.String())
}

func downloadDigitalShareImage(
	ctx context.Context,
	client *http.Client,
	imageURL string,
	maxBytes int,
) (digitalShareImageAttachment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return digitalShareImageAttachment{}, fmt.Errorf("build image request: %w", err)
	}
	req.Header.Set("User-Agent", digitalShareFetcherUserAgent)
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return digitalShareImageAttachment{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return digitalShareImageAttachment{}, fmt.Errorf("status %d: %s", resp.StatusCode, compactWhitespace(string(body)))
	}

	if maxBytes <= 0 {
		maxBytes = maxDigitalShareImageBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes+1)))
	if err != nil {
		return digitalShareImageAttachment{}, fmt.Errorf("read image: %w", err)
	}
	if len(data) > maxBytes {
		return digitalShareImageAttachment{}, fmt.Errorf("image exceeded %d bytes", maxBytes)
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	if contentType == "" {
		contentType = strings.ToLower(http.DetectContentType(data))
	}
	if !isSupportedDigitalShareImageContentType(contentType) {
		detected := strings.ToLower(http.DetectContentType(data))
		if !isSupportedDigitalShareImageContentType(detected) {
			return digitalShareImageAttachment{}, fmt.Errorf("unsupported content type %q", contentType)
		}
		contentType = detected
	}

	parsed, err := neturl.Parse(imageURL)
	if err != nil {
		return digitalShareImageAttachment{}, fmt.Errorf("invalid image url: %w", err)
	}
	return buildDigitalShareRemoteAttachment(parsed, contentType, data)
}

func buildDigitalShareRemoteAttachment(
	imageURL *neturl.URL,
	contentType string,
	data []byte,
) (digitalShareImageAttachment, error) {
	if len(data) == 0 {
		return digitalShareImageAttachment{}, fmt.Errorf("empty image body")
	}
	sum := sha256.Sum256(data)
	return digitalShareImageAttachment{
		Bytes:     data,
		MimeType:  clampStr(strings.TrimSpace(contentType), 128),
		Filename:  clampStr(guessDigitalShareFilename(imageURL, contentType), 255),
		SHA256Hex: hex.EncodeToString(sum[:]),
	}, nil
}

func isSupportedDigitalShareImageContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "image/")
}

func guessDigitalShareFilename(imageURL *neturl.URL, contentType string) string {
	filename := ""
	if imageURL != nil {
		filename = strings.TrimSpace(path.Base(imageURL.Path))
	}
	if filename == "" || filename == "." || filename == "/" {
		filename = "shared-remote-image"
	}
	if ext := path.Ext(filename); ext == "" {
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			filename += exts[0]
		}
	}
	return filename
}
