package lark

import (
	"encoding/json"
	"fmt"
)

// EventKind tags the kind of decoded event a handler is dealing with.
// EventUnknown covers anything we receive but don't care to act on
// (non-text messages, unsupported event types) so handlers can ack 200
// without branching on a long list of negatives.
type EventKind int

const (
	EventUnknown EventKind = iota
	EventURLVerification
	EventMessageReceive
)

// Event is the high-level decoded result of a single Lark webhook POST.
// Fields outside the relevant Kind are zero-valued; handlers should
// switch on Kind before reading payload fields.
type Event struct {
	Kind EventKind

	// URL verification handshake.
	Token     string // verification token claimed by Lark
	Challenge string // value to echo back

	// im.message.receive_v1.
	AppID        string
	EventID      string
	SenderOpenID string
	ChatID       string
	MessageID    string
	Text         string // empty if the body wasn't a text message
}

// Decode parses a raw, already-signature-verified body into an Event.
// Callers must verify the signature first via VerifySignature; Decode
// trusts what it's given.
func Decode(body []byte) (Event, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Event{}, fmt.Errorf("invalid envelope: %w", err)
	}
	if env.Type == "url_verification" {
		return Event{
			Kind:      EventURLVerification,
			Token:     env.Token,
			Challenge: env.Challenge,
		}, nil
	}
	if env.Header.EventType != "im.message.receive_v1" {
		return Event{Kind: EventUnknown}, nil
	}
	var msg messageEvent
	if err := json.Unmarshal(env.Event, &msg); err != nil {
		return Event{}, fmt.Errorf("parse message event: %w", err)
	}
	if msg.Message.MessageType != "text" {
		return Event{Kind: EventUnknown}, nil
	}
	return Event{
		Kind:         EventMessageReceive,
		AppID:        env.Header.AppID,
		EventID:      env.Header.EventID,
		SenderOpenID: msg.Sender.SenderID.OpenID,
		ChatID:       msg.Message.ChatID,
		MessageID:    msg.Message.MessageID,
		Text:         extractText(msg.Message.Content),
	}, nil
}

// extractText unwraps Lark's JSON-in-JSON text content, which for text
// messages looks like {"text":"hello"}. Returns "" on parse failure so
// the caller can treat the event as ignorable.
func extractText(content string) string {
	var c struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &c); err != nil {
		return ""
	}
	return c.Text
}

// envelope is the outer wrapper Lark uses for both url_verification
// and v2 event subscriptions. The Type field only exists on the
// handshake payload; real events leave it empty and populate Header.
type envelope struct {
	Type      string `json:"type"`
	Token     string `json:"token"`
	Challenge string `json:"challenge"`
	Schema    string `json:"schema"`
	Header    struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		AppID     string `json:"app_id"`
		Token     string `json:"token"`
	} `json:"header"`
	Event json.RawMessage `json:"event"`
}

type messageEvent struct {
	Sender struct {
		SenderID struct {
			OpenID string `json:"open_id"`
		} `json:"sender_id"`
	} `json:"sender"`
	Message struct {
		MessageID   string `json:"message_id"`
		ChatID      string `json:"chat_id"`
		MessageType string `json:"message_type"`
		Content     string `json:"content"` // JSON-in-JSON, e.g. {"text":"..."}
	} `json:"message"`
}
