package mailtransport

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
	"unicode"
)

type entity struct {
	headers textproto.MIMEHeader
	body    []byte
}

func encodeMessage(message Message) ([]byte, string, string, []string, error) {
	from, err := emailAddress(message.FromEmail)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("invalid FromEmail: %w", err)
	}
	if err := headerValue(message.FromName); err != nil {
		return nil, "", "", nil, fmt.Errorf("invalid FromName: %w", err)
	}
	if err := headerValue(message.Subject); err != nil {
		return nil, "", "", nil, fmt.Errorf("invalid Subject: %w", err)
	}
	if len(message.To) == 0 || len(message.To) > 100 {
		return nil, "", "", nil, fmt.Errorf("SMTP message must have between 1 and 100 recipients")
	}
	recipients := make([]string, 0, len(message.To))
	toHeaders := make([]string, 0, len(message.To))
	for _, value := range message.To {
		address, err := emailAddress(value)
		if err != nil {
			return nil, "", "", nil, fmt.Errorf("invalid recipient email address: %w", err)
		}
		recipients = append(recipients, address)
		toHeaders = append(toHeaders, (&mail.Address{Address: address}).String())
	}

	var body entity
	if message.Text != "" && message.HTML != "" {
		text, err := textEntity("text/plain", message.Text)
		if err != nil {
			return nil, "", "", nil, err
		}
		html, err := textEntity("text/html", message.HTML)
		if err != nil {
			return nil, "", "", nil, err
		}
		body, err = multipartEntity("alternative", []entity{text, html})
	} else if message.HTML != "" {
		body, err = textEntity("text/html", message.HTML)
	} else {
		body, err = textEntity("text/plain", message.Text)
	}
	if err != nil {
		return nil, "", "", nil, err
	}
	var inline, attachments []entity
	for _, attachment := range message.Attachments {
		part, err := attachmentEntity(attachment)
		if err != nil {
			return nil, "", "", nil, err
		}
		if attachment.Inline {
			inline = append(inline, part)
		} else {
			attachments = append(attachments, part)
		}
	}
	if len(inline) > 0 {
		body, err = multipartEntity("related", append([]entity{body}, inline...))
		if err != nil {
			return nil, "", "", nil, err
		}
	}
	if len(attachments) > 0 {
		body, err = multipartEntity("mixed", append([]entity{body}, attachments...))
		if err != nil {
			return nil, "", "", nil, err
		}
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, "", "", nil, fmt.Errorf("generate message ID: %w", err)
	}
	id := "<" + hex.EncodeToString(token) + "@" + from[strings.LastIndexByte(from, '@')+1:] + ">"
	var wire bytes.Buffer
	writeHeader(&wire, "From", (&mail.Address{Name: message.FromName, Address: from}).String())
	writeHeader(&wire, "To", strings.Join(toHeaders, ", "))
	writeHeader(&wire, "Subject", mime.QEncoding.Encode("UTF-8", message.Subject))
	writeHeader(&wire, "Date", time.Now().Format(time.RFC1123Z))
	writeHeader(&wire, "Message-ID", id)
	writeHeader(&wire, "MIME-Version", "1.0")
	for _, key := range []string{"Content-Type", "Content-Transfer-Encoding"} {
		if value := body.headers.Get(key); value != "" {
			writeHeader(&wire, key, value)
		}
	}
	wire.WriteString("\r\n")
	wire.Write(body.body)
	return wire.Bytes(), id, from, recipients, nil
}

func headerValue(value string) error {
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("header contains control characters")
		}
	}
	return nil
}

func emailAddress(value string) (string, error) {
	if err := headerValue(value); err != nil {
		return "", err
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || !strings.Contains(value, "@") {
		return "", fmt.Errorf("expected a bare email address")
	}
	for _, r := range address.Address {
		if r > unicode.MaxASCII {
			return "", fmt.Errorf("SMTPUTF8 email addresses are not supported")
		}
	}
	return address.Address, nil
}

// Fold at whitespace without splitting encoded words or address tokens.
func writeHeader(w *bytes.Buffer, key, value string) {
	lineLength := len(key) + 2
	w.WriteString(key + ": ")
	for i, token := range strings.Split(value, " ") {
		if i > 0 {
			if lineLength+1+len(token) > 78 {
				w.WriteString("\r\n ")
				lineLength = 1
			} else {
				w.WriteByte(' ')
				lineLength++
			}
		}
		w.WriteString(token)
		lineLength += len(token)
	}
	w.WriteString("\r\n")
}

func textEntity(contentType, value string) (entity, error) {
	var body bytes.Buffer
	writer := quotedprintable.NewWriter(&body)
	// Normalize line endings before quoted-printable encoding.
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	if _, err := writer.Write([]byte(strings.ReplaceAll(value, "\n", "\r\n"))); err != nil {
		return entity{}, fmt.Errorf("encode text MIME: %w", err)
	}
	if err := writer.Close(); err != nil {
		return entity{}, fmt.Errorf("finish text MIME: %w", err)
	}
	return entity{headers: textproto.MIMEHeader{
		"Content-Type":              {contentType + "; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	}, body: body.Bytes()}, nil
}

func multipartEntity(subtype string, children []entity) (entity, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, child := range children {
		part, err := writer.CreatePart(child.headers)
		if err != nil {
			return entity{}, fmt.Errorf("create MIME part: %w", err)
		}
		if _, err := part.Write(child.body); err != nil {
			return entity{}, fmt.Errorf("write MIME part: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return entity{}, fmt.Errorf("finish MIME multipart: %w", err)
	}
	return entity{headers: textproto.MIMEHeader{
		"Content-Type": {mime.FormatMediaType("multipart/"+subtype, map[string]string{"boundary": writer.Boundary()})},
	}, body: body.Bytes()}, nil
}

func attachmentEntity(attachment Attachment) (entity, error) {
	if err := headerValue(attachment.Filename); err != nil {
		return entity{}, fmt.Errorf("invalid attachment filename: %w", err)
	}
	if attachment.Filename == "" {
		return entity{}, fmt.Errorf("attachment filename is required")
	}
	contentType := attachment.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if err := headerValue(contentType); err != nil {
		return entity{}, fmt.Errorf("invalid attachment content type: %w", err)
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.Contains(mediaType, "/") {
		return entity{}, fmt.Errorf("invalid attachment content type")
	}
	params["name"] = attachment.Filename
	disposition := "attachment"
	if attachment.Inline {
		disposition = "inline"
		if attachment.ContentID == "" {
			return entity{}, fmt.Errorf("inline attachment ContentID is required")
		}
	}
	headers := textproto.MIMEHeader{
		"Content-Type":              {mime.FormatMediaType(mediaType, params)},
		"Content-Disposition":       {mime.FormatMediaType(disposition, map[string]string{"filename": attachment.Filename})},
		"Content-Transfer-Encoding": {"base64"},
	}
	if attachment.ContentID != "" {
		cid := strings.TrimSuffix(strings.TrimPrefix(attachment.ContentID, "<"), ">")
		if cid == "" || strings.ContainsAny(cid, "<> \t\r\n") || headerValue(cid) != nil {
			return entity{}, fmt.Errorf("invalid attachment ContentID")
		}
		for _, r := range cid {
			if r > unicode.MaxASCII {
				return entity{}, fmt.Errorf("attachment ContentID must be ASCII")
			}
		}
		headers.Set("Content-ID", "<"+cid+">")
	}
	encoded := base64.StdEncoding.EncodeToString(attachment.Data)
	var body bytes.Buffer
	for len(encoded) > 76 {
		body.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	if encoded != "" {
		body.WriteString(encoded + "\r\n")
	}
	return entity{headers: headers, body: body.Bytes()}, nil
}
