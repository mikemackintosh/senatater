package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMessage_PlainText(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: hello\r\n" +
		"Date: Mon, 01 Jan 2024 10:00:00 -0500\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Just a quick note about the project.\r\n"

	m, err := parseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "hello" {
		t.Errorf("subject: %q", m.Subject)
	}
	if !strings.Contains(m.Body, "quick note") {
		t.Errorf("body: %q", m.Body)
	}
}

func TestParseMessage_MultipartAlternative_PrefersPlain(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: status\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"BOUND\"\r\n" +
		"\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello in plain text.\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>Hello in <b>HTML</b>.</p>\r\n" +
		"--BOUND--\r\n"

	m, err := parseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Body, "Hello in plain text") {
		t.Errorf("expected plain part chosen; got %q", m.Body)
	}
	if strings.Contains(m.Body, "<b>") {
		t.Errorf("HTML leaked into body: %q", m.Body)
	}
}

func TestParseMessage_MultipartAlternative_HTMLFallback(t *testing.T) {
	raw := "Subject: html only\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"X\"\r\n" +
		"\r\n" +
		"--X\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<html><body><p>Q3 numbers&nbsp;look <i>great</i>.</p></body></html>\r\n" +
		"--X--\r\n"

	m, err := parseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.Body, "<") {
		t.Errorf("tags not stripped: %q", m.Body)
	}
	if !strings.Contains(m.Body, "Q3 numbers") || !strings.Contains(m.Body, "great") {
		t.Errorf("text content lost: %q", m.Body)
	}
}

func TestParseMessage_QuotedPrintable(t *testing.T) {
	raw := "Subject: qp\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Caf=C3=A9 at 3=3D the corner.\r\n"

	m, err := parseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Body, "Café") {
		t.Errorf("QP not decoded: %q", m.Body)
	}
	if !strings.Contains(m.Body, "3= the corner") {
		t.Errorf("QP equals literal not decoded: %q", m.Body)
	}
}

func TestParseMessage_Base64(t *testing.T) {
	// "Top secret memo." base64-encoded
	raw := "Subject: b64\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"VG9wIHNlY3JldCBtZW1vLg==\r\n"

	m, err := parseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Body, "Top secret memo") {
		t.Errorf("base64 not decoded: %q", m.Body)
	}
}

func TestParseMessage_CapturesMessageID(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: hi\r\n" +
		"Message-ID: <abc123@example.com>\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"body\r\n"
	m, err := parseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageID != "<abc123@example.com>" {
		t.Errorf("MessageID: %q", m.MessageID)
	}
}

func TestParseMessage_RFC2047Subject(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: =?UTF-8?B?Q2Fmw6kgbWVldGluZw==?=\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"body\r\n"

	m, err := parseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Café meeting" {
		t.Errorf("subject not decoded: %q", m.Subject)
	}
}

func TestParseMessage_MultipartMixed_SkipsAttachmentBinary(t *testing.T) {
	// Attachment is base64 binary noise; we should keep the text part and drop the binary.
	raw := "Subject: with attach\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"M\"\r\n" +
		"\r\n" +
		"--M\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"See attached.\r\n" +
		"--M\r\n" +
		"Content-Type: application/octet-stream; name=blob.bin\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=\r\n" +
		"--M--\r\n"

	m, err := parseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Body, "See attached") {
		t.Errorf("text part lost: %q", m.Body)
	}
	// We didn't keep the octet-stream part: any of its decoded bytes (0x00..0x1f) would be noise.
	if strings.ContainsAny(m.Body, "\x00\x01\x02") {
		t.Errorf("binary leaked into body")
	}
}

func TestMBOX_SplitsOnFromLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mbox")
	mbox := "From alice@example.com Mon Jan  1 10:00:00 2024\r\n" +
		"From: alice@example.com\r\n" +
		"Subject: one\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"first message body\r\n" +
		"\r\n" +
		"From bob@example.com Tue Jan  2 11:00:00 2024\r\n" +
		"From: bob@example.com\r\n" +
		"Subject: two\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"second message body\r\n"

	if err := os.WriteFile(path, []byte(mbox), 0o644); err != nil {
		t.Fatal(err)
	}

	var got []Message
	if err := MBOX(path, func(m Message) error {
		got = append(got, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 messages, got %d", len(got))
	}
	if got[0].Subject != "one" || got[1].Subject != "two" {
		t.Errorf("subjects: %q, %q", got[0].Subject, got[1].Subject)
	}
	if !strings.Contains(got[0].Body, "first message") {
		t.Errorf("first body: %q", got[0].Body)
	}
	if !strings.Contains(got[1].Body, "second message") {
		t.Errorf("second body: %q", got[1].Body)
	}
}

func TestHTMLToText(t *testing.T) {
	in := `<html><head><style>p{color:red}</style><script>alert(1)</script></head>` +
		`<body><p>Hello&nbsp;<b>world</b>.</p><p>Line two.</p></body></html>`
	got := htmlToText(in)
	if strings.Contains(got, "<") || strings.Contains(got, "alert") || strings.Contains(got, "color:red") {
		t.Errorf("dirty: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "world") || !strings.Contains(got, "Line two") {
		t.Errorf("content lost: %q", got)
	}
}
