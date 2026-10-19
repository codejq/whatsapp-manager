package rules

import (
	"testing"
	"time"
)

func TestNormalizeText(t *testing.T) {
	cases := map[string]string{
		"عَاجِل":    "عاجل",
		"عـــاجل":   "عاجل",
		"أحمد":      "احمد",
		"إلى":       "الي",
		"مدرسة":     "مدرسه",
		"URGENT":    "urgent",
		"١٢٣":       "123",
		"Ｆｕｌｌｗｉｄｔｈ": "fullwidth",
	}
	for in, want := range cases {
		if got := NormalizeText(in); got != want {
			t.Errorf("NormalizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContainsLink(t *testing.T) {
	yes := []string{
		"see https://example.com/x",
		"http://a.b",
		"www.google.com",
		"visit example.com today",
		"رابط: shop.example.sa/p/1",
		"join chat.whatsapp.com/AbC123",
	}
	no := []string{"hello world", "price is 3.50", "file.pdf attached", "مرحبا"}
	for _, s := range yes {
		if !ContainsLink(s) {
			t.Errorf("expected link in %q", s)
		}
	}
	for _, s := range no {
		if ContainsLink(s) {
			t.Errorf("unexpected link in %q", s)
		}
	}
}

func TestContainsWord(t *testing.T) {
	if !containsWord(NormalizeText("هذا طلب عاجل جدا"), NormalizeText("عاجل")) {
		t.Fatal("whole word not found")
	}
	if containsWord("urgently needed", "urgent") {
		t.Fatal("partial word matched as whole word")
	}
	if !containsWord("urgent!", "urgent") {
		t.Fatal("punctuation boundary failed")
	}
}

func baseMsg() *Message {
	return &Message{
		Event: "message", ID: "M1", ChatID: "120363000000000001@g.us",
		From: "966500000001@s.whatsapp.net", Body: "Hello",
		Timestamp: time.Now(), Type: "text",
	}
}

func baseRule() *Rule {
	return &Rule{
		ID: "r1", Name: "r", Enabled: true, CreatedAt: time.Now().Add(-time.Hour).UnixMilli(),
		Source:  Source{Kind: SourceAny},
		Actions: []Action{{Type: ActionEmail, Emails: []string{"a@b.com"}}},
	}
}

func TestEvaluate(t *testing.T) {
	now := time.Now()
	ctx := EvalContext{Now: now, MaxAge: 12 * time.Hour}

	tests := []struct {
		name   string
		mutR   func(*Rule)
		mutM   func(*Message)
		want   bool
		reason string
	}{
		{"any matches", nil, nil, true, ""},
		{"disabled", func(r *Rule) { r.Enabled = false }, nil, false, ReasonDisabled},
		{"own message ignored", nil, func(m *Message) { m.IsFromMe = true }, false, ReasonOwnMessage},
		{"own message opt-in", func(r *Rule) { r.Options.IncludeOwnMessages = true }, func(m *Message) { m.IsFromMe = true }, true, ""},
		{"too old", nil, func(m *Message) { m.Timestamp = now.Add(-13 * time.Hour) }, false, ReasonBeforeRule},
		{"too old catchup", func(r *Rule) { r.CreatedAt = now.Add(-48 * time.Hour).UnixMilli() }, func(m *Message) { m.Timestamp = now.Add(-13 * time.Hour) }, false, ReasonTooOld},
		{"before rule", func(r *Rule) { r.CreatedAt = now.UnixMilli() }, func(m *Message) { m.Timestamp = now.Add(-10 * time.Minute) }, false, ReasonBeforeRule},
		{"groups only - group", func(r *Rule) { r.Source.Kind = SourceGroups }, nil, true, ""},
		{"private only - group", func(r *Rule) { r.Source.Kind = SourcePrivate }, nil, false, ReasonSource},
		{"specific chat", func(r *Rule) {
			r.Source = Source{Kind: SourceChats, Chats: []Peer{{JID: "120363000000000001@g.us"}}}
		}, nil, true, ""},
		{"specific chat other", func(r *Rule) {
			r.Source = Source{Kind: SourceChats, Chats: []Peer{{JID: "999@g.us"}}}
		}, nil, false, ReasonSource},
		{"sender by phone", func(r *Rule) {
			r.Source.Senders = []Peer{{JID: "966500000001@s.whatsapp.net"}}
		}, nil, true, ""},
		{"sender by lid", func(r *Rule) {
			r.Source.Senders = []Peer{{JID: "111@lid"}}
		}, func(m *Message) { m.FromLID = "111@lid" }, true, ""},
		{"sender other", func(r *Rule) {
			r.Source.Senders = []Peer{{JID: "966500000009@s.whatsapp.net"}}
		}, nil, false, ReasonSender},
		{"link yes", func(r *Rule) {
			r.Conditions.Items = []Condition{{Type: CondContainsLink}}
		}, func(m *Message) { m.Body = "check https://x.com" }, true, ""},
		{"link no", func(r *Rule) {
			r.Conditions.Items = []Condition{{Type: CondContainsLink}}
		}, nil, false, ReasonConditions},
		{"arabic keyword with diacritics", func(r *Rule) {
			r.Conditions.Items = []Condition{{Type: CondKeywords, Words: []string{"عاجل"}}}
		}, func(m *Message) { m.Body = "طلب عَاجِل" }, true, ""},
		{"keyword all mode", func(r *Rule) {
			r.Conditions.Items = []Condition{{Type: CondKeywords, Words: []string{"order", "paid"}, Mode: "all"}}
		}, func(m *Message) { m.Body = "Order is PAID" }, true, ""},
		{"keyword all mode missing", func(r *Rule) {
			r.Conditions.Items = []Condition{{Type: CondKeywords, Words: []string{"order", "paid"}, Mode: "all"}}
		}, func(m *Message) { m.Body = "order pending" }, false, ReasonConditions},
		{"not contains", func(r *Rule) {
			r.Conditions.Items = []Condition{{Type: CondNotContains, Words: []string{"spam"}}}
		}, func(m *Message) { m.Body = "this is spam" }, false, ReasonConditions},
		{"type image caption keyword", func(r *Rule) {
			r.Conditions.Items = []Condition{{Type: CondMessageType, Types: []string{"image"}}, {Type: CondKeywords, Words: []string{"invoice"}}}
		}, func(m *Message) {
			m.Type = "image"
			m.Body = ""
			m.Media = &Media{Kind: "image", Caption: "Invoice #4"}
		}, true, ""},
		{"any-of conditions", func(r *Rule) {
			r.Conditions = Conditions{Match: "any", Items: []Condition{{Type: CondContainsLink}, {Type: CondKeywords, Words: []string{"hello"}}}}
		}, nil, true, ""},
		{"document filename keyword", func(r *Rule) {
			r.Conditions.Items = []Condition{{Type: CondKeywords, Words: []string{"report"}}}
		}, func(m *Message) {
			m.Type = "document"
			m.Body = ""
			m.Media = &Media{Kind: "document", Filename: "Monthly-Report.pdf"}
		}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := baseRule()
			m := baseMsg()
			if tc.mutR != nil {
				tc.mutR(r)
			}
			if tc.mutM != nil {
				tc.mutM(m)
			}
			r.Normalize()
			got := Evaluate(r, m, ctx)
			if got.Matched != tc.want || (!tc.want && tc.reason != "" && got.Reason != tc.reason) {
				t.Fatalf("got matched=%v reason=%q, want matched=%v reason=%q", got.Matched, got.Reason, tc.want, tc.reason)
			}
		})
	}
}

func TestParseWebhook(t *testing.T) {
	raw := []byte(`{"event":"message","device_id":"966@s.whatsapp.net","payload":{
		"id":"ABC","chat_id":"120@g.us","from":"9665@s.whatsapp.net","from_lid":"77@lid",
		"sender_display_name":"Ali","timestamp":"2026-10-15T10:30:00Z","is_from_me":false,
		"body":"see doc","document":{"path":"statics/media/x.pdf","caption":"see doc"}}}`)
	m, err := ParseWebhook(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "ABC" || m.Type != "document" || m.Media == nil || m.Media.Path != "statics/media/x.pdf" || m.SenderName != "Ali" || m.FromLID != "77@lid" {
		t.Fatalf("unexpected parse: %+v media=%+v", m, m.Media)
	}
	raw2 := []byte(`{"event":"message","device_id":"d","payload":{"id":"X","chat_id":"1@s.whatsapp.net","from":"1@s.whatsapp.net","audio":"statics/media/a.ogg"}}`)
	m2, _ := ParseWebhook(raw2)
	if m2.Type != "audio" || m2.Media.Path != "statics/media/a.ogg" || m2.SenderName != "1" {
		t.Fatalf("unexpected audio parse: %+v", m2)
	}
}

func TestValidate(t *testing.T) {
	r := baseRule()
	r.Normalize()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Actions = []Action{{Type: ActionEmail, Emails: []string{"not-an-email"}}}
	if err := r.Validate(); err == nil {
		t.Fatal("expected invalid email")
	}
	r.Actions = []Action{{Type: ActionWhatsApp, To: &Peer{JID: "966500000000@s.whatsapp.net"}}}
	r.Normalize()
	if err := r.Validate(); err != nil || r.Actions[0].Mode != ModeForward {
		t.Fatal("whatsapp action should validate with default forward mode", err)
	}
}
