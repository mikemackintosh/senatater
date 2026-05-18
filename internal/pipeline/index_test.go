package pipeline

import (
	"strings"
	"testing"

	"emails-rag/internal/extract"
)

func TestMessageDedupID_PrefersMessageID(t *testing.T) {
	m := extract.Message{
		MessageID: "<abc123@example.com>",
		From:      "alice@example.com",
		Subject:   "hi",
		Date:      "Mon, 01 Jan 2024 10:00:00 -0500",
		Body:      "body",
	}
	if got := MessageDedupID(m); got != "<abc123@example.com>" {
		t.Errorf("want raw Message-ID, got %q", got)
	}
}

func TestMessageDedupID_FingerprintFallback(t *testing.T) {
	m := extract.Message{
		From:    "alice@example.com",
		Subject: "hi",
		Date:    "Mon, 01 Jan 2024 10:00:00 -0500",
		Body:    "the body",
	}
	id := MessageDedupID(m)
	if !strings.HasPrefix(id, "fp:") {
		t.Errorf("want fp: prefix, got %q", id)
	}
	// Same inputs must yield the same fingerprint.
	if id2 := MessageDedupID(m); id2 != id {
		t.Errorf("fingerprint not stable: %q vs %q", id, id2)
	}
}

func TestMessageDedupID_FingerprintsDifferOnSubject(t *testing.T) {
	base := extract.Message{
		From: "a@b.com", Date: "Mon, 01 Jan 2024 10:00:00 -0500", Body: "x",
	}
	a := base
	a.Subject = "one"
	b := base
	b.Subject = "two"
	if MessageDedupID(a) == MessageDedupID(b) {
		t.Error("fingerprints collided on differing subjects")
	}
}
