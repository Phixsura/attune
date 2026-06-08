// SPDX-License-Identifier: Apache-2.0

package email

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed testdata/simple.eml
var simpleEML []byte

//go:embed testdata/threaded.eml
var threadedEML []byte

//go:embed testdata/multipart.eml
var multipartEML []byte

//go:embed testdata/bad-mime.eml
var badMIMEEML []byte

func TestParseRFC822_Simple(t *testing.T) {
	p, err := parseRFC822(simpleEML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.From != "alice@example.com" {
		t.Errorf("From = %q; want alice@example.com", p.From)
	}
	if p.Subject != "Buggy login flow" {
		t.Errorf("Subject = %q", p.Subject)
	}
	if p.MessageID != "<abc123@example.com>" {
		t.Errorf("MessageID = %q", p.MessageID)
	}
	if !strings.Contains(p.TextBody, "Safari") {
		t.Errorf("TextBody missing keyword: %q", p.TextBody)
	}
}

func TestParseRFC822_Threaded(t *testing.T) {
	p, err := parseRFC822(threadedEML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.InReplyTo != "<abc123@example.com>" {
		t.Errorf("InReplyTo = %q", p.InReplyTo)
	}
	if len(p.References) != 1 || p.References[0] != "<abc123@example.com>" {
		t.Errorf("References = %v", p.References)
	}
}

func TestParseRFC822_MultipartAlternative_PrefersText(t *testing.T) {
	p, err := parseRFC822(multipartEML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(p.TextBody, "Plain version") {
		t.Errorf("TextBody should prefer plain alt: %q", p.TextBody)
	}
	if strings.Contains(p.TextBody, "<b>") {
		t.Errorf("TextBody must not contain HTML tags: %q", p.TextBody)
	}
}

// Deliberately malformed MIME — bad-mime.eml has an unterminated
// boundary string. The parser may either error or return an empty body;
// either is acceptable as long as it does not panic.
func TestParseRFC822_BadMIME_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on bad MIME: %v", r)
		}
	}()
	_, _ = parseRFC822(badMIMEEML)
}
