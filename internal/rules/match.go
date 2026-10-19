package rules

import (
	"strings"
	"time"
)

// Skip reasons (i18n keys shown in the activity log).
const (
	ReasonOwnMessage = "activity.reason.own_message"
	ReasonTooOld     = "activity.reason.too_old"
	ReasonBeforeRule = "activity.reason.before_rule"
	ReasonSource     = "activity.reason.source"
	ReasonSender     = "activity.reason.sender"
	ReasonConditions = "activity.reason.conditions"
	ReasonDisabled   = "activity.reason.disabled"
)

// EvalContext holds inputs outside the message itself.
type EvalContext struct {
	Now          time.Time
	MaxAge       time.Duration // catch-up window; 0 disables the check
	ChatNameHint string
}

// Result explains how one rule evaluated against a message.
type Result struct {
	RuleID  string `json:"rule_id"`
	Name    string `json:"name"`
	Matched bool   `json:"matched"`
	Reason  string `json:"reason,omitempty"`
}

// Evaluate returns whether rule r fires for message m, with a reason when it
// does not. It is pure and safe to call concurrently.
func Evaluate(r *Rule, m *Message, ctx EvalContext) Result {
	res := Result{RuleID: r.ID, Name: r.Name}
	if !r.Enabled {
		res.Reason = ReasonDisabled
		return res
	}
	if m.IsFromMe && !r.Options.IncludeOwnMessages {
		res.Reason = ReasonOwnMessage
		return res
	}
	if !m.Timestamp.IsZero() {
		if r.CreatedAt > 0 && m.Timestamp.UnixMilli() < r.CreatedAt-60_000 { // 1 min grace for clock skew
			res.Reason = ReasonBeforeRule
			return res
		}
		if ctx.MaxAge > 0 && ctx.Now.Sub(m.Timestamp) > ctx.MaxAge {
			res.Reason = ReasonTooOld
			return res
		}
	}
	if !sourceMatches(&r.Source, m) {
		res.Reason = ReasonSource
		return res
	}
	if len(r.Source.Senders) > 0 && !anyPeerMatches(r.Source.Senders, m.From, m.FromLID) {
		res.Reason = ReasonSender
		return res
	}
	if !conditionsMatch(&r.Conditions, m) {
		res.Reason = ReasonConditions
		return res
	}
	res.Matched = true
	return res
}

func sourceMatches(s *Source, m *Message) bool {
	switch s.Kind {
	case SourceAny, "":
		return m.IsGroup() || m.IsPrivate()
	case SourceGroups:
		return m.IsGroup()
	case SourcePrivate:
		return m.IsPrivate()
	case SourceChats:
		return anyPeerMatches(s.Chats, m.ChatID, m.ChatLID)
	}
	return false
}

// anyPeerMatches compares JIDs leniently: exact JID, LID, or same phone number
// for user JIDs (so "966500000000@s.whatsapp.net" matches regardless of the
// device suffix or LID migration).
func anyPeerMatches(peers []Peer, jid, lid string) bool {
	for _, p := range peers {
		if PeerMatches(p, jid, lid) {
			return true
		}
	}
	return false
}

// PeerMatches reports whether the peer refers to jid/lid.
func PeerMatches(p Peer, jid, lid string) bool {
	if p.JID != "" && (strings.EqualFold(p.JID, jid) || strings.EqualFold(p.JID, lid)) {
		return true
	}
	if p.LID != "" && (strings.EqualFold(p.LID, lid) || strings.EqualFold(p.LID, jid)) {
		return true
	}
	// Phone-number comparison for user JIDs only (never groups).
	if strings.HasSuffix(p.JID, "@s.whatsapp.net") && strings.HasSuffix(jid, "@s.whatsapp.net") {
		return userPart(p.JID) == userPart(jid)
	}
	return false
}

func conditionsMatch(c *Conditions, m *Message) bool {
	if len(c.Items) == 0 {
		return true
	}
	text := NormalizeText(m.Text())
	rawText := m.Text()
	anyMode := c.Match == "any"
	for _, item := range c.Items {
		ok := conditionMatches(&item, m, text, rawText)
		if anyMode && ok {
			return true
		}
		if !anyMode && !ok {
			return false
		}
	}
	return !anyMode
}

func conditionMatches(c *Condition, m *Message, text, rawText string) bool {
	switch c.Type {
	case CondContainsLink:
		return ContainsLink(rawText)
	case CondKeywords:
		if len(c.Words) == 0 {
			return false
		}
		all := c.Mode == "all"
		for _, w := range c.Words {
			hit := wordIn(text, NormalizeText(w), c.WholeWord)
			if all && !hit {
				return false
			}
			if !all && hit {
				return true
			}
		}
		return all
	case CondNotContains:
		for _, w := range c.Words {
			if wordIn(text, NormalizeText(w), c.WholeWord) {
				return false
			}
		}
		return true
	case CondMessageType:
		for _, t := range c.Types {
			if t == m.Type {
				return true
			}
		}
		return false
	}
	return false
}

func wordIn(text, word string, whole bool) bool {
	word = strings.TrimSpace(word)
	if word == "" {
		return false
	}
	if whole {
		return containsWord(text, word)
	}
	return strings.Contains(text, word)
}
