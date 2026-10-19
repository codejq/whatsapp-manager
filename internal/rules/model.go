// Package rules defines forwarding rules and the pure matching logic that
// decides which rules fire for an incoming message.
package rules

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Source kinds.
const (
	SourceAny     = "any"     // every chat
	SourceGroups  = "groups"  // any group
	SourcePrivate = "private" // any 1:1 chat
	SourceChats   = "chats"   // only the chats listed in Source.Chats
)

// Condition types.
const (
	CondContainsLink = "contains_link"
	CondKeywords     = "keywords"
	CondNotContains  = "not_contains"
	CondMessageType  = "message_type"
)

// Action types and WhatsApp send modes.
const (
	ActionWhatsApp = "whatsapp"
	ActionEmail    = "email"
	ModeForward    = "forward"
	ModeCopy       = "copy_with_header"
)

// Message types reported by MessageType().
var MessageTypes = []string{"text", "image", "video", "document", "audio", "sticker", "location", "contact", "poll", "other"}

// Peer is a chat or person picked in the UI.
type Peer struct {
	JID  string `json:"jid"`
	LID  string `json:"lid,omitempty"`
	Name string `json:"name"`
}

// Source selects where a message comes from.
type Source struct {
	Kind    string `json:"kind"`
	Chats   []Peer `json:"chats,omitempty"`
	Senders []Peer `json:"senders,omitempty"` // optional: only these senders
}

// Condition is one test on the message content.
type Condition struct {
	Type      string   `json:"type"`
	Words     []string `json:"words,omitempty"`
	Mode      string   `json:"mode,omitempty"` // keywords: any | all
	WholeWord bool     `json:"whole_word,omitempty"`
	Types     []string `json:"types,omitempty"` // message_type
}

// Conditions combines condition items with all/any logic.
type Conditions struct {
	Match string      `json:"match"` // all | any
	Items []Condition `json:"items"`
}

// Action is what happens when the rule matches.
type Action struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	To           *Peer    `json:"to,omitempty"`   // whatsapp
	Mode         string   `json:"mode,omitempty"` // whatsapp: forward | copy_with_header
	Emails       []string `json:"emails,omitempty"`
	IncludeMedia bool     `json:"include_media,omitempty"`
}

// Options are rare tweaks.
type Options struct {
	IncludeOwnMessages bool `json:"include_own_messages,omitempty"`
	CooldownSeconds    int  `json:"cooldown_seconds,omitempty"`
}

// Rule is a complete forwarding rule.
type Rule struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	Priority   int        `json:"priority"`
	CreatedAt  int64      `json:"created_at"`
	UpdatedAt  int64      `json:"updated_at"`
	Source     Source     `json:"source"`
	Conditions Conditions `json:"conditions"`
	Actions    []Action   `json:"actions"`
	Options    Options    `json:"options"`
}

// ValidationError carries an i18n key so the UI can show a translated message.
type ValidationError struct {
	Key   string
	Field string
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s (%s)", e.Key, e.Field) }

func verr(key, field string) error { return &ValidationError{Key: key, Field: field} }

// Normalize fills defaults and trims user input in place.
func (r *Rule) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
	if r.Source.Kind == "" {
		r.Source.Kind = SourceAny
	}
	if r.Conditions.Match != "any" {
		r.Conditions.Match = "all"
	}
	items := r.Conditions.Items[:0]
	for _, c := range r.Conditions.Items {
		c.Words = cleanList(c.Words)
		if c.Type == CondKeywords && c.Mode != "all" {
			c.Mode = "any"
		}
		items = append(items, c)
	}
	r.Conditions.Items = items
	for i := range r.Actions {
		a := &r.Actions[i]
		if a.Type == ActionWhatsApp && a.Mode != ModeCopy {
			a.Mode = ModeForward
		}
		if a.Type == ActionEmail {
			out := make([]string, 0, len(a.Emails))
			for _, e := range cleanList(a.Emails) {
				out = append(out, strings.ToLower(e))
			}
			a.Emails = out
		}
		if a.To != nil {
			a.To.JID = strings.TrimSpace(a.To.JID)
		}
	}
	if r.Options.CooldownSeconds < 0 {
		r.Options.CooldownSeconds = 0
	}
}

// Validate checks the rule is complete and consistent.
func (r *Rule) Validate() error {
	if r.Name == "" {
		return verr("errors.rule.name_required", "name")
	}
	switch r.Source.Kind {
	case SourceAny, SourceGroups, SourcePrivate:
	case SourceChats:
		if len(r.Source.Chats) == 0 {
			return verr("errors.rule.chats_required", "source.chats")
		}
	default:
		return verr("errors.rule.bad_source", "source.kind")
	}
	for _, c := range r.Conditions.Items {
		switch c.Type {
		case CondContainsLink:
		case CondKeywords, CondNotContains:
			if len(c.Words) == 0 {
				return verr("errors.rule.words_required", "conditions")
			}
		case CondMessageType:
			if len(c.Types) == 0 {
				return verr("errors.rule.types_required", "conditions")
			}
		default:
			return verr("errors.rule.bad_condition", "conditions")
		}
	}
	if len(r.Actions) == 0 {
		return verr("errors.rule.action_required", "actions")
	}
	for _, a := range r.Actions {
		switch a.Type {
		case ActionWhatsApp:
			if a.To == nil || a.To.JID == "" {
				return verr("errors.rule.wa_target_required", "actions")
			}
			if !strings.Contains(a.To.JID, "@") {
				return verr("errors.rule.wa_target_invalid", "actions")
			}
		case ActionEmail:
			if len(a.Emails) == 0 {
				return verr("errors.rule.email_required", "actions")
			}
			for _, e := range a.Emails {
				if _, err := mail.ParseAddress(e); err != nil || !strings.Contains(e, "@") {
					return verr("errors.rule.email_invalid", "actions")
				}
			}
		default:
			return verr("errors.rule.bad_action", "actions")
		}
	}
	return nil
}

// HasEmailAction reports whether any action sends email.
func (r *Rule) HasEmailAction() bool {
	for _, a := range r.Actions {
		if a.Type == ActionEmail {
			return true
		}
	}
	return false
}

// ErrNotFound is returned when a rule does not exist.
var ErrNotFound = errors.New("rule not found")

func cleanList(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
