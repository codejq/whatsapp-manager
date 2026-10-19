package engine_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"whatsapp-manager/internal/dispatch"
	"whatsapp-manager/internal/engine"
	"whatsapp-manager/internal/gowa"
	"whatsapp-manager/internal/mail"
	"whatsapp-manager/internal/rules"
	"whatsapp-manager/internal/store"
	"whatsapp-manager/internal/webhook"
)

type fakeWA struct {
	mu       sync.Mutex
	sent     []string // "to|kind|id"
	failNext error
	n        int
}

func (f *fakeWA) record(to, kind string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return "", err
	}
	f.n++
	id := fmt.Sprintf("OUT%d", f.n)
	f.sent = append(f.sent, to+"|"+kind+"|"+id)
	return id, nil
}
func (f *fakeWA) Forward(_ context.Context, _, to string) (string, error) {
	return f.record(to, "forward")
}
func (f *fakeWA) SendText(_ context.Context, to, _ string) (string, error) {
	return f.record(to, "text")
}
func (f *fakeWA) SendMedia(_ context.Context, kind, to, _, _ string) (string, error) {
	return f.record(to, kind)
}
func (f *fakeWA) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sent) }

type harness struct {
	st     *store.Store
	eng    *engine.Engine
	disp   *dispatch.Dispatcher
	wa     *fakeWA
	hook   http.Handler
	mu     sync.Mutex
	emails []mail.Email
	secret string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &harness{st: st, wa: &fakeWA{}, secret: "s3cret"}
	h.eng = engine.New(st, func() engine.Options { return engine.Options{MaxAge: 12 * time.Hour} }, log, nil)
	h.disp = dispatch.New(dispatch.Deps{
		Store:       st,
		WA:          func() dispatch.WhatsApp { return h.wa },
		WAConnected: func() bool { return true },
		SMTP: func() (mail.Settings, string) {
			return mail.Settings{Host: "smtp.example.com", FromAddress: "bot@example.com"}, dispatch.SMTPOK
		},
		SendEmail: func(_ context.Context, _ mail.Settings, e mail.Email) error {
			h.mu.Lock()
			h.emails = append(h.emails, e)
			h.mu.Unlock()
			return nil
		},
		WARate:         func() int { return 6000 },
		SubjectCounter: func() bool { return true },
		EmailRate:      func() int { return 6000 },
		Log:            log,
	})
	h.hook = webhook.Handler(st, func() string { return h.secret }, h.eng.Notify, log)
	return h
}

func (h *harness) deliver(t *testing.T, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(body))
	req.Header.Set("X-Hub-Signature-256", webhook.Sign([]byte(body), h.secret))
	rec := httptest.NewRecorder()
	h.hook.ServeHTTP(rec, req)
	return rec.Code
}

// drain runs the engine and dispatcher until no work is left.
func (h *harness) drain(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		if _, err := h.eng.ProcessBatch(ctx); err != nil {
			t.Fatal(err)
		}
	}
	dctx, dcancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer dcancel()
	h.disp.Run(dctx)
}

func msgJSON(id, chat, from, body string, fromMe bool) string {
	return fmt.Sprintf(`{"event":"message","device_id":"966500000000@s.whatsapp.net","payload":{"id":%q,"chat_id":%q,"from":%q,"sender_display_name":"Ali","timestamp":%q,"is_from_me":%v,"body":%q}}`,
		id, chat, from, time.Now().UTC().Format(time.RFC3339), fromMe, body)
}

func saveRule(t *testing.T, st *store.Store, r *rules.Rule) {
	t.Helper()
	r.Normalize()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.CreatedAt = time.Now().Add(-time.Hour).UnixMilli()
	if err := st.SaveRule(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestEndToEndNoDuplicates(t *testing.T) {
	h := newHarness(t)
	group := "120363000000000001@g.us"
	target := rules.Peer{JID: "966511111111@s.whatsapp.net", Name: "Boss"}
	// Two rules that both match and both forward to the same person + email.
	saveRule(t, h.st, &rules.Rule{ID: "r1", Name: "Links", Enabled: true,
		Source:     rules.Source{Kind: rules.SourceChats, Chats: []rules.Peer{{JID: group}}},
		Conditions: rules.Conditions{Items: []rules.Condition{{Type: rules.CondContainsLink}}},
		Actions:    []rules.Action{{Type: rules.ActionWhatsApp, To: &target}, {Type: rules.ActionEmail, Emails: []string{"Leads@Example.com"}}},
	})
	saveRule(t, h.st, &rules.Rule{ID: "r2", Name: "Urgent", Enabled: true,
		Source:     rules.Source{Kind: rules.SourceAny},
		Conditions: rules.Conditions{Items: []rules.Condition{{Type: rules.CondKeywords, Words: []string{"عاجل"}}}},
		Actions:    []rules.Action{{Type: rules.ActionWhatsApp, To: &target, Mode: rules.ModeCopy}, {Type: rules.ActionEmail, Emails: []string{"leads@example.com"}}},
	})

	body := msgJSON("MSG1", group, "966522222222@s.whatsapp.net", "عَاجِل https://example.com/offer", false)
	// GoWA retries: deliver the same event three times.
	for i := 0; i < 3; i++ {
		if code := h.deliver(t, body); code != 200 {
			t.Fatalf("webhook code %d", code)
		}
	}
	h.drain(t)
	h.drain(t) // a second pass must not produce anything new

	if got := h.wa.count(); got != 1 {
		t.Fatalf("whatsapp sends = %d, want exactly 1 (%v)", got, h.wa.sent)
	}
	if len(h.emails) != 1 {
		t.Fatalf("emails = %d, want exactly 1", len(h.emails))
	}
	if e := h.emails[0]; !strings.Contains(e.Text, "s-i-u.com") || !strings.Contains(e.HTML, "s-i-u.com") ||
		strings.Index(e.HTML, "s-i-u.com") < strings.Index(e.HTML, "example.com/offer") {
		t.Fatal("sponsor ad missing or not at the end of the email")
	}
	if !strings.HasPrefix(h.emails[0].Subject, "[WhatsApp #1] ") {
		t.Fatalf("subject without counter: %q", h.emails[0].Subject)
	}

	// Loop guard: the message we sent comes back as an incoming event.
	echo := msgJSON("OUT1", "966511111111@s.whatsapp.net", "966511111111@s.whatsapp.net", "عاجل https://example.com/offer", false)
	h.deliver(t, echo)
	// Own messages are ignored by default.
	own := msgJSON("MINE1", group, "966500000000@s.whatsapp.net", "عاجل https://x.com", true)
	h.deliver(t, own)
	h.drain(t)
	if got := h.wa.count(); got != 1 {
		t.Fatalf("loop guard failed: sends = %d (%v)", got, h.wa.sent)
	}
}

// WhatsApp can deliver a group message as two webhook events with the same
// ID: an empty sender-key carrier first, then the real text. The real one
// must not be dropped as a duplicate.
func TestEmptyCarrierThenRealMessage(t *testing.T) {
	h := newHarness(t)
	group := "120363419648840728@g.us"
	saveRule(t, h.st, &rules.Rule{ID: "r1", Name: "Links", Enabled: true,
		Source:     rules.Source{Kind: rules.SourceChats, Chats: []rules.Peer{{JID: group}}},
		Conditions: rules.Conditions{Items: []rules.Condition{{Type: rules.CondContainsLink}}},
		Actions:    []rules.Action{{Type: rules.ActionEmail, Emails: []string{"a@example.com"}}},
	})
	ts := time.Now().UTC().Format(time.RFC3339)
	empty := fmt.Sprintf(`{"event":"message","device_id":"d","payload":{"id":"3EB0X","chat_id":%q,"from":"966570529330@s.whatsapp.net","timestamp":%q,"is_from_me":false}}`, group, ts)
	real := msgJSON("3EB0X", group, "966570529330@s.whatsapp.net", "https://us05web.zoom.us/j/87200890466?pwd=abc", false)
	if h.deliver(t, empty) != 200 || h.deliver(t, real) != 200 {
		t.Fatal("webhook rejected")
	}
	h.drain(t)
	if len(h.emails) != 1 {
		t.Fatalf("emails = %d, want 1 (real message was dropped)", len(h.emails))
	}
}

func TestBadSignatureRejected(t *testing.T) {
	h := newHarness(t)
	body := msgJSON("X", "1@s.whatsapp.net", "1@s.whatsapp.net", "hi", false)
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()
	h.hook.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestOfflineDoesNotConsumeAttempts(t *testing.T) {
	h := newHarness(t)
	target := rules.Peer{JID: "966511111111@s.whatsapp.net"}
	saveRule(t, h.st, &rules.Rule{ID: "r1", Name: "All", Enabled: true, Source: rules.Source{Kind: rules.SourcePrivate},
		Actions: []rules.Action{{Type: rules.ActionWhatsApp, To: &target}}})
	h.deliver(t, msgJSON("M1", "966533333333@s.whatsapp.net", "966533333333@s.whatsapp.net", "hello", false))
	h.wa.failNext = fmt.Errorf("%w: down", gowa.ErrUnavailable)
	h.drain(t)
	acts, _ := h.st.Activity(context.Background(), store.ActivityFilter{})
	if len(acts) != 1 || len(acts[0].Deliveries) != 1 {
		t.Fatalf("unexpected activity: %+v", acts)
	}
	d := acts[0].Deliveries[0]
	if d.Status != "pending" || d.Attempts != 0 {
		t.Fatalf("offline job: status=%s attempts=%d", d.Status, d.Attempts)
	}
}

func TestSameChatNotEchoed(t *testing.T) {
	h := newHarness(t)
	group := "120363000000000009@g.us"
	saveRule(t, h.st, &rules.Rule{ID: "r1", Name: "Echo", Enabled: true, Source: rules.Source{Kind: rules.SourceAny},
		Actions: []rules.Action{{Type: rules.ActionWhatsApp, To: &rules.Peer{JID: group}}}})
	h.deliver(t, msgJSON("M1", group, "966533333333@s.whatsapp.net", "hello", false))
	h.drain(t)
	if h.wa.count() != 0 {
		t.Fatal("message was sent back into its own chat")
	}
}

func TestPermanentFailure(t *testing.T) {
	h := newHarness(t)
	target := rules.Peer{JID: "966511111111@s.whatsapp.net"}
	saveRule(t, h.st, &rules.Rule{ID: "r1", Name: "All", Enabled: true, Source: rules.Source{Kind: rules.SourcePrivate},
		Actions: []rules.Action{{Type: rules.ActionWhatsApp, To: &target, Mode: rules.ModeCopy}}})
	h.deliver(t, msgJSON("M1", "966533333333@s.whatsapp.net", "966533333333@s.whatsapp.net", "hello", false))
	h.wa.failNext = &gowa.APIError{Status: 400, Code: "BAD_REQUEST", Message: "invalid phone"}
	h.drain(t)
	acts, _ := h.st.Activity(context.Background(), store.ActivityFilter{})
	if d := acts[0].Deliveries[0]; d.Status != "failed" {
		t.Fatalf("status=%s want failed", d.Status)
	}
	var ae *gowa.APIError
	_ = errors.As(h.wa.failNext, &ae)
}
