package extract

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"regexp"
	"strings"
)

// Message represents a single parsed email from an MBOX file.
type Message struct {
	From      string
	To        string
	Subject   string
	Date      string
	MessageID string
	Body      string
}

// MBOX parses an MBOX file and yields each message via the callback.
// Streams line by line rather than loading the entire archive into memory.
func MBOX(path string, fn func(Message) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var buf bytes.Buffer
	flush := func() error {
		if buf.Len() == 0 {
			return nil
		}
		msg, err := parseMessage(buf.Bytes())
		buf.Reset()
		if err != nil {
			return nil
		}
		return fn(msg)
	}

	for s.Scan() {
		line := s.Bytes()
		// Detects message boundaries via the standard "From " separator.
		if bytes.HasPrefix(line, []byte("From ")) && buf.Len() > 0 {
			if err := flush(); err != nil {
				return err
			}
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := s.Err(); err != nil && err != io.EOF {
		return err
	}
	return flush()
}

// parseMessage converts a raw RFC 5322 message into a Message, walking
// MIME parts and decoding transfer encodings so the body is readable text.
func parseMessage(raw []byte) (Message, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Message{}, err
	}
	body, err := extractBody(textproto(msg.Header), msg.Body)
	if err != nil {
		// Fall back to the raw body so we still index *something* on parse error.
		rawBody, _ := io.ReadAll(msg.Body)
		body = string(rawBody)
	}
	return Message{
		From:      decodeHeader(msg.Header.Get("From")),
		To:        decodeHeader(msg.Header.Get("To")),
		Subject:   decodeHeader(msg.Header.Get("Subject")),
		Date:      msg.Header.Get("Date"),
		MessageID: strings.TrimSpace(msg.Header.Get("Message-ID")),
		Body:      strings.TrimSpace(body),
	}, nil
}

// textproto is a tiny adapter so we can reuse the same Content-Type / encoding
// lookup logic for both top-level and per-part headers.
type headerLookup interface {
	Get(string) string
}

func textproto(h mail.Header) headerLookup { return mailHeader(h) }

type mailHeader mail.Header

func (h mailHeader) Get(k string) string { return mail.Header(h).Get(k) }

// extractBody picks a textual representation from possibly multipart content,
// honoring Content-Transfer-Encoding on every leaf part.
func extractBody(header headerLookup, body io.Reader) (string, error) {
	ct := header.Get("Content-Type")
	if ct == "" {
		// No Content-Type: treat as plain text with whatever transfer encoding is set.
		return decodeEncoded(body, header.Get("Content-Transfer-Encoding"))
	}

	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return decodeEncoded(body, header.Get("Content-Transfer-Encoding"))
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return decodeEncoded(body, header.Get("Content-Transfer-Encoding"))
		}
		return extractMultipart(body, boundary, mediaType)
	}

	decoded, err := decodeEncoded(body, header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(mediaType, "text/html") {
		return htmlToText(decoded), nil
	}
	return decoded, nil
}

// extractMultipart walks a multipart body. For multipart/alternative it prefers
// text/plain and only falls back to HTML if no plain part exists. For other
// multipart subtypes (mixed, related, signed, ...) it concatenates all text
// parts in document order so attachments don't leak binary noise into the index.
func extractMultipart(body io.Reader, boundary, mediaType string) (string, error) {
	mr := multipart.NewReader(body, boundary)
	var plain, htmlParts, nested []string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		partCT := part.Header.Get("Content-Type")
		partMedia, partParams, _ := mime.ParseMediaType(partCT)

		if strings.HasPrefix(partMedia, "multipart/") {
			sub, err := extractMultipart(part, partParams["boundary"], partMedia)
			part.Close()
			if err == nil && sub != "" {
				nested = append(nested, sub)
			}
			continue
		}

		decoded, err := decodeEncoded(part, part.Header.Get("Content-Transfer-Encoding"))
		part.Close()
		if err != nil {
			continue
		}

		switch {
		case strings.HasPrefix(partMedia, "text/plain"):
			plain = append(plain, decoded)
		case strings.HasPrefix(partMedia, "text/html"):
			htmlParts = append(htmlParts, htmlToText(decoded))
		}
	}

	if strings.Contains(mediaType, "alternative") {
		if len(plain) > 0 {
			return strings.Join(plain, "\n\n"), nil
		}
		if len(htmlParts) > 0 {
			return strings.Join(htmlParts, "\n\n"), nil
		}
		if len(nested) > 0 {
			return strings.Join(nested, "\n\n"), nil
		}
		return "", nil
	}

	out := make([]string, 0, len(plain)+len(htmlParts)+len(nested))
	out = append(out, plain...)
	out = append(out, htmlParts...)
	out = append(out, nested...)
	return strings.Join(out, "\n\n"), nil
}

// decodeEncoded applies the named Content-Transfer-Encoding and reads to string.
func decodeEncoded(r io.Reader, encoding string) (string, error) {
	var src io.Reader = r
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		src = quotedprintable.NewReader(r)
	case "base64":
		src = base64.NewDecoder(base64.StdEncoding, r)
	}
	b, err := io.ReadAll(src)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeHeader resolves RFC 2047 encoded-words ("=?UTF-8?B?...?=") into UTF-8.
func decodeHeader(s string) string {
	if s == "" {
		return s
	}
	dec := new(mime.WordDecoder)
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

var (
	// RE2 has no backreferences, so script/style are matched separately.
	htmlScript     = regexp.MustCompile(`(?is)<script[^>]*>.*?</\s*script\s*>`)
	htmlStyle      = regexp.MustCompile(`(?is)<style[^>]*>.*?</\s*style\s*>`)
	htmlBlockBreak = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|h[1-6]|blockquote|pre|table|hr|article|section)[^>]*>`)
	htmlAnyTag     = regexp.MustCompile(`<[^>]+>`)
	htmlWhitespace = regexp.MustCompile(`[ \t]+`)
	htmlNewlines   = regexp.MustCompile(`\n{3,}`)
)

// htmlToText is a pragmatic, dependency-free HTML stripper for email bodies.
// It is not a general-purpose HTML parser; it assumes the well-formed HTML
// typical of mail clients and prioritizes producing readable plain text.
func htmlToText(s string) string {
	s = htmlScript.ReplaceAllString(s, "")
	s = htmlStyle.ReplaceAllString(s, "")
	s = htmlBlockBreak.ReplaceAllString(s, "\n")
	s = htmlAnyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = htmlWhitespace.ReplaceAllString(s, " ")
	s = htmlNewlines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
