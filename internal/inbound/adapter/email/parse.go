// SPDX-License-Identifier: Apache-2.0

package email

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // register non-utf8 decoders
)

// parsedMail is the subset of RFC822 fields the email adapter cares
// about. Multipart/alternative bodies prefer the text/plain part; only
// when no plain part exists does the parser fall through to the whole
// body (which may be the only HTML payload).
type parsedMail struct {
	From       string
	Subject    string
	MessageID  string
	InReplyTo  string
	References []string
	TextBody   string
}

// parseRFC822 parses raw RFC822 bytes (as fetched from IMAP) into the
// adapter's parsedMail shape. Returns an error if the MIME envelope is
// malformed enough that even the headers can't be read; downstream
// callers handle "headers ok, body fishy" by advancing the cursor with
// validate_err metrics instead of wedging the source.
func parseRFC822(raw []byte) (parsedMail, error) {
	msg, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return parsedMail{}, err
	}

	p := parsedMail{
		From:      strings.TrimSpace(msg.Header.Get("From")),
		Subject:   strings.TrimSpace(msg.Header.Get("Subject")),
		MessageID: strings.TrimSpace(msg.Header.Get("Message-Id")),
		InReplyTo: strings.TrimSpace(msg.Header.Get("In-Reply-To")),
	}
	if refs := strings.TrimSpace(msg.Header.Get("References")); refs != "" {
		p.References = strings.Fields(refs)
	}

	if mr := msg.MultipartReader(); mr != nil {
		// Prefer text/plain inside a multipart. Stop at first match.
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return p, err
			}
			ct, _, _ := part.Header.ContentType()
			if ct == "text/plain" {
				body, err := io.ReadAll(part.Body)
				if err != nil {
					return p, err
				}
				p.TextBody = string(body)
				return p, nil
			}
		}
		// No text/plain part found — leave TextBody empty rather than
		// hand a raw HTML blob to the LLM downstream.
		return p, nil
	}

	body, err := io.ReadAll(msg.Body)
	if err != nil {
		return p, err
	}
	p.TextBody = string(body)
	return p, nil
}
