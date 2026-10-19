package rules

import (
	"encoding/json"
	"strings"
	"time"
)

// Media describes an attachment that GoWA downloaded (Path, relative to the
// GoWA working directory) or only referenced (URL).
type Media struct {
	Kind     string `json:"kind"`
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
	Caption  string `json:"caption,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// Message is the subset of a GoWA "message" webhook event the rules use.
type Message struct {
	Event       string    `json:"event"`
	DeviceID    string    `json:"device_id"`
	ID          string    `json:"id"`
	ChatID      string    `json:"chat_id"`
	ChatLID     string    `json:"chat_lid,omitempty"`
	From        string    `json:"from"`
	FromLID     string    `json:"from_lid,omitempty"`
	SenderName  string    `json:"sender_name"`
	Timestamp   time.Time `json:"timestamp"`
	IsFromMe    bool      `json:"is_from_me"`
	Body        string    `json:"body"`
	QuotedBody  string    `json:"quoted_body,omitempty"`
	Forwarded   bool      `json:"forwarded,omitempty"`
	Media       *Media    `json:"media,omitempty"`
	Type        string    `json:"type"`
	HasLocation bool      `json:"-"`
}

// IsGroup reports whether the chat is a group.
func (m *Message) IsGroup() bool { return strings.HasSuffix(m.ChatID, "@g.us") }

// IsPrivate reports whether the chat is a 1:1 chat.
func (m *Message) IsPrivate() bool {
	return strings.HasSuffix(m.ChatID, "@s.whatsapp.net") || strings.HasSuffix(m.ChatID, "@lid")
}

// Text returns all searchable text of the message.
func (m *Message) Text() string {
	parts := []string{m.Body}
	if m.Media != nil {
		if m.Media.Caption != "" && m.Media.Caption != m.Body {
			parts = append(parts, m.Media.Caption)
		}
		if m.Media.Filename != "" {
			parts = append(parts, m.Media.Filename)
		}
	}
	return strings.Join(parts, "\n")
}

type webhookEnvelope struct {
	Event    string          `json:"event"`
	DeviceID string          `json:"device_id"`
	Payload  json.RawMessage `json:"payload"`
}

// ParseWebhook parses a raw GoWA webhook body. It tolerates the shape
// variations GoWA uses for media (a string path, or an object with path/url/caption).
func ParseWebhook(raw []byte) (*Message, error) {
	var env webhookEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	var p map[string]any
	if len(env.Payload) > 0 {
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return nil, err
		}
	}
	m := &Message{Event: env.Event, DeviceID: env.DeviceID}
	m.ID = str(p["id"])
	m.ChatID = str(p["chat_id"])
	m.ChatLID = str(p["chat_lid"])
	m.From = str(p["from"])
	m.FromLID = str(p["from_lid"])
	m.SenderName = firstNonEmpty(str(p["sender_display_name"]), str(p["from_name"]), userPart(m.From))
	m.Body = str(p["body"])
	m.QuotedBody = str(p["quoted_body"])
	if b, ok := p["is_from_me"].(bool); ok {
		m.IsFromMe = b
	}
	if b, ok := p["forwarded"].(bool); ok {
		m.Forwarded = b
	}
	if ts := str(p["timestamp"]); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			m.Timestamp = t
		}
	}
	m.Type = "other"
	for _, kind := range []string{"image", "video", "video_note", "document", "audio", "sticker"} {
		v, ok := p[kind]
		if !ok || v == nil {
			continue
		}
		md := &Media{Kind: kind}
		switch t := v.(type) {
		case string:
			md.Path = t
		case map[string]any:
			md.Path = str(t["path"])
			md.URL = str(t["url"])
			md.Caption = str(t["caption"])
			md.Filename = str(t["filename"])
		}
		m.Media = md
		switch kind {
		case "video_note":
			m.Type = "video"
		default:
			m.Type = kind
		}
		break
	}
	if m.Media == nil {
		switch {
		case p["location"] != nil || p["live_location"] != nil:
			m.Type = "location"
			m.HasLocation = true
		case p["contact"] != nil || p["contacts_array"] != nil:
			m.Type = "contact"
		case p["poll"] != nil:
			m.Type = "poll"
		case m.Body != "":
			m.Type = "text"
		}
	}
	return m, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// userPart returns the part of a JID before '@' (the phone number for users).
func userPart(jid string) string {
	if i := strings.IndexByte(jid, '@'); i >= 0 {
		jid = jid[:i]
	}
	if i := strings.IndexByte(jid, ':'); i >= 0 { // device suffix
		jid = jid[:i]
	}
	return jid
}

// UserPart is exported for other packages.
func UserPart(jid string) string { return userPart(jid) }
