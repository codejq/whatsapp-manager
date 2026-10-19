// Package dispatch delivers outbox jobs over WhatsApp and email with
// retries, rate limits and the at-most-once crash guarantee.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"whatsapp-manager/internal/gowa"
	"whatsapp-manager/internal/mail"
	"whatsapp-manager/internal/rules"
	"whatsapp-manager/internal/store"
)

// MaxAttempts is the total number of attempts before a job fails.
const MaxAttempts = 6

// Backoff is the delay after attempt n (1-based).
var Backoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// MaxAttachment is the largest file attached to an email.
const MaxAttachment = 20 << 20

// WhatsApp is the subset of the GoWA client used for sending.
type WhatsApp interface {
	Forward(ctx context.Context, messageID, to string) (string, error)
	SendText(ctx context.Context, to, text string) (string, error)
	SendMedia(ctx context.Context, kind, to, caption, filePath string) (string, error)
}

// SMTPState values.
const (
	SMTPNotConfigured = "not_configured"
	SMTPUntested      = "untested"
	SMTPOK            = "ok"
	SMTPAuthFailed    = "auth_failed"
)

// Deps are the dispatcher's collaborators. Functions are read on every job,
// so settings changes apply without a restart.
type Deps struct {
	Store       *store.Store
	WA          func() WhatsApp
	WAConnected func() bool
	SMTP        func() (mail.Settings, string) // settings, state
	OnAuthFail  func(err error)                // marks SMTP state auth_failed and notifies
	GowaDir     string
	Lang        func() string
	WARate      func() int // messages per minute
	EmailRate   func() int
	Log         *slog.Logger
	// SendEmail is overridable for tests.
	SendEmail func(ctx context.Context, s mail.Settings, e mail.Email) error
}

// Dispatcher runs the WhatsApp and email workers.
type Dispatcher struct {
	d       Deps
	wakeWA  chan struct{}
	wakeMail chan struct{}
}

// New creates a dispatcher.
func New(d Deps) *Dispatcher {
	if d.SendEmail == nil {
		d.SendEmail = mail.Send
	}
	return &Dispatcher{d: d, wakeWA: make(chan struct{}, 1), wakeMail: make(chan struct{}, 1)}
}

// Wake nudges both workers.
func (x *Dispatcher) Wake() {
	for _, c := range []chan struct{}{x.wakeWA, x.wakeMail} {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

// Run starts both workers and blocks until ctx is done.
func (x *Dispatcher) Run(ctx context.Context) {
	done := make(chan struct{}, 2)
	go func() { x.loop(ctx, "whatsapp", x.wakeWA); done <- struct{}{} }()
	go func() { x.loop(ctx, "email", x.wakeMail); done <- struct{}{} }()
	<-done
	<-done
}

func (x *Dispatcher) loop(ctx context.Context, channel string, wake chan struct{}) {
	var last time.Time
	for ctx.Err() == nil {
		worked := x.safeStep(ctx, channel, &last)
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(3 * time.Second):
		}
	}
}

func (x *Dispatcher) safeStep(ctx context.Context, channel string, last *time.Time) (worked bool) {
	defer func() {
		if r := recover(); r != nil {
			x.d.Log.Error("dispatch: panic", "channel", channel, "panic", r)
			worked = false
		}
	}()
	return x.step(ctx, channel, last)
}

// step handles at most one job; it returns true if it did work.
func (x *Dispatcher) step(ctx context.Context, channel string, last *time.Time) bool {
	if channel == "whatsapp" && !x.d.WAConnected() {
		return false // jobs stay pending until WhatsApp is back
	}
	// Rate limit: minimum spacing between sends, with jitter for WhatsApp.
	rate := 20
	if channel == "whatsapp" && x.d.WARate != nil {
		rate = x.d.WARate()
	}
	if channel == "email" {
		rate = 30
		if x.d.EmailRate != nil {
			rate = x.d.EmailRate()
		}
	}
	if rate < 1 {
		rate = 1
	}
	gap := time.Minute / time.Duration(rate)
	if channel == "whatsapp" {
		gap += time.Duration(rand.IntN(1000)) * time.Millisecond
	}
	if wait := gap - time.Since(*last); wait > 0 {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(wait):
		}
	}

	job, ok, err := x.d.Store.ClaimNextOutbox(ctx, channel)
	if err != nil {
		x.d.Log.Error("dispatch: claim failed", "err", err)
		return false
	}
	if !ok {
		return false
	}
	*last = time.Now()
	payload, err := x.d.Store.GetInboxPayload(ctx, job.InboxID)
	if err != nil {
		_ = x.d.Store.MarkFailed(ctx, job.ID, "activity.reason.source_missing")
		return true
	}
	msg, err := rules.ParseWebhook([]byte(payload))
	if err != nil {
		_ = x.d.Store.MarkFailed(ctx, job.ID, "activity.reason.parse_error")
		return true
	}
	switch channel {
	case "whatsapp":
		x.sendWhatsApp(ctx, job, msg)
	case "email":
		x.sendEmail(ctx, job, msg)
	}
	return true
}

func (x *Dispatcher) retryOrFail(ctx context.Context, job store.OutboxItem, errText string) {
	if job.Attempts >= MaxAttempts {
		_ = x.d.Store.MarkFailed(ctx, job.ID, errText)
		return
	}
	i := job.Attempts - 1
	if i < 0 {
		i = 0
	}
	if i >= len(Backoff) {
		i = len(Backoff) - 1
	}
	_ = x.d.Store.Reschedule(ctx, job.ID, time.Now().Add(Backoff[i]), errText, false)
}

// ---------- WhatsApp ----------

func (x *Dispatcher) sendWhatsApp(ctx context.Context, job store.OutboxItem, msg *rules.Message) {
	a := job.Action
	if a.To == nil || a.To.JID == "" {
		_ = x.d.Store.MarkFailed(ctx, job.ID, "errors.rule.wa_target_required")
		return
	}
	wa := x.d.WA()
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var sentID string
	var err error
	if a.Mode == rules.ModeCopy {
		sentID, err = x.copySend(sctx, wa, a.To.JID, msg)
	} else {
		sentID, err = wa.Forward(sctx, msg.ID, a.To.JID)
		if err != nil && !gowa.IsTransient(err) {
			// The original may be missing from GoWA's storage: fall back to a copy.
			x.d.Log.Warn("dispatch: forward failed, falling back to copy", "job", job.ID, "err", err)
			sentID, err = x.copySend(sctx, wa, a.To.JID, msg)
		}
	}
	if err == nil {
		_ = x.d.Store.MarkSent(ctx, job.ID, sentID)
		x.d.Log.Info("dispatch: whatsapp sent", "job", job.ID, "to", a.To.JID)
		return
	}
	x.d.Log.Warn("dispatch: whatsapp send failed", "job", job.ID, "attempt", job.Attempts, "err", err)
	switch {
	case errors.Is(err, gowa.ErrNotLoggedIn) || errors.Is(err, gowa.ErrUnavailable):
		// Not the job's fault: wait for WhatsApp without consuming attempts.
		_ = x.d.Store.Reschedule(ctx, job.ID, time.Now().Add(30*time.Second), "errors.wa.offline", true)
	case gowa.IsTransient(err):
		x.retryOrFail(ctx, job, err.Error())
	default:
		_ = x.d.Store.MarkFailed(ctx, job.ID, err.Error())
	}
}

func (x *Dispatcher) copySend(ctx context.Context, wa WhatsApp, to string, msg *rules.Message) (string, error) {
	header := Header(x.lang(), msg, x.d.Store.ChatName(ctx, msg.ChatID))
	body := msg.Body
	if msg.Media != nil {
		if p := x.mediaPath(msg.Media); p != "" {
			caption := header
			if c := firstNonEmpty(msg.Media.Caption, msg.Body); c != "" {
				caption += "\n\n" + c
			}
			kind := msg.Media.Kind
			switch kind {
			case "image", "video", "audio":
			case "video_note":
				kind = "video"
			default:
				kind = "file"
			}
			id, err := wa.SendMedia(ctx, kind, to, caption, p)
			if err == nil && kind == "audio" {
				// Audio has no caption: send the header as text too.
				_, _ = wa.SendText(ctx, to, header)
			}
			return id, err
		}
	}
	text := header
	if body != "" {
		text += "\n\n" + body
	} else if msg.Media != nil {
		text += "\n\n" + mediaLabel(x.lang(), msg.Media.Kind)
	}
	return wa.SendText(ctx, to, text)
}

func (x *Dispatcher) mediaPath(m *rules.Media) string {
	if m == nil || m.Path == "" || x.d.GowaDir == "" {
		return ""
	}
	p := m.Path
	if !filepath.IsAbs(p) {
		p = filepath.Join(x.d.GowaDir, filepath.FromSlash(p))
	}
	p = filepath.Clean(p)
	rel, err := filepath.Rel(filepath.Clean(x.d.GowaDir), p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "" // never read files outside GoWA's directory
	}
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return ""
	}
	return p
}

func (x *Dispatcher) lang() string {
	if x.d.Lang == nil {
		return "ar"
	}
	return x.d.Lang()
}

// ---------- Email ----------

func (x *Dispatcher) sendEmail(ctx context.Context, job store.OutboxItem, msg *rules.Message) {
	settings, state := x.d.SMTP()
	if !settings.Configured() || state == SMTPNotConfigured || state == SMTPUntested || state == SMTPAuthFailed {
		_ = x.d.Store.MarkWaitingEmail(ctx, job.ID, "errors.smtp.not_ready")
		return
	}
	e := x.BuildEmail(ctx, job, msg)
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := x.d.SendEmail(sctx, settings, e)
	if err == nil {
		_ = x.d.Store.MarkSent(ctx, job.ID, "")
		x.d.Log.Info("dispatch: email sent", "job", job.ID, "to", e.To)
		return
	}
	key, detail := mail.Describe(err)
	x.d.Log.Warn("dispatch: email failed", "job", job.ID, "attempt", job.Attempts, "key", key, "detail", detail)
	var me *mail.Error
	if errors.As(mail.Classify(err), &me) {
		switch {
		case me.Kind == mail.KindAuth || me.Kind == mail.KindConfig:
			_ = x.d.Store.MarkWaitingEmail(ctx, job.ID, key)
			if x.d.OnAuthFail != nil {
				x.d.OnAuthFail(err)
			}
			return
		case me.Transient():
			x.retryOrFail(ctx, job, key)
			return
		}
	}
	_ = x.d.Store.MarkFailed(ctx, job.ID, key+": "+detail)
}

// BuildEmail renders the email for a job.
func (x *Dispatcher) BuildEmail(ctx context.Context, job store.OutboxItem, msg *rules.Message) mail.Email {
	lang := x.lang()
	chat := x.d.Store.ChatName(ctx, msg.ChatID)
	if chat == "" {
		if msg.IsPrivate() {
			chat = msg.SenderName
		} else {
			chat = rules.UserPart(msg.ChatID)
		}
	}
	subject := fmt.Sprintf("[WhatsApp] %s — %s", chat, msg.SenderName)
	if !msg.IsGroup() {
		subject = fmt.Sprintf("[WhatsApp] %s", msg.SenderName)
	}
	ts := msg.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	tsText := ts.Local().Format("2006-01-02 15:04")
	labels := emailLabels[lang]
	if labels == nil {
		labels = emailLabels["en"]
	}
	body := msg.Body
	if body == "" && msg.Media != nil {
		body = firstNonEmpty(msg.Media.Caption, mediaLabel(lang, msg.Media.Kind))
	}
	sender := msg.SenderName
	if phone := rules.UserPart(msg.From); phone != "" && phone != sender && strings.HasSuffix(msg.From, "@s.whatsapp.net") {
		sender += " (+" + phone + ")"
	}

	text := fmt.Sprintf("%s: %s\n%s: %s\n%s: %s\n%s: %s\n\n%s\n", labels["from"], sender, labels["chat"], chat, labels["time"], tsText, labels["rule"], job.RuleName, body)

	dir := "ltr"
	if lang == "ar" {
		dir = "rtl"
	}
	var links strings.Builder
	escBody := html.EscapeString(body)
	for _, l := range rules.FindLinks(body) {
		href := l
		if !strings.HasPrefix(strings.ToLower(href), "http") {
			href = "https://" + href
		}
		links.WriteString(fmt.Sprintf(`<li><a href="%s">%s</a></li>`, html.EscapeString(href), html.EscapeString(l)))
	}
	linksHTML := ""
	if links.Len() > 0 {
		linksHTML = `<p style="margin:12px 0 4px;color:#555">` + labels["links"] + `</p><ul style="margin:0;padding-inline-start:20px">` + links.String() + `</ul>`
	}
	h := fmt.Sprintf(`<!doctype html><html dir="%s"><body style="font-family:Segoe UI,Tahoma,Arial,sans-serif;font-size:14px;color:#111">
<table style="border-collapse:collapse;margin-bottom:12px">
<tr><td style="color:#666;padding:2px 8px">%s</td><td style="padding:2px 8px"><b>%s</b></td></tr>
<tr><td style="color:#666;padding:2px 8px">%s</td><td style="padding:2px 8px">%s</td></tr>
<tr><td style="color:#666;padding:2px 8px">%s</td><td style="padding:2px 8px">%s</td></tr>
<tr><td style="color:#666;padding:2px 8px">%s</td><td style="padding:2px 8px">%s</td></tr>
</table>
<div dir="auto" style="white-space:pre-wrap;background:#f4f6f5;border-radius:8px;padding:12px;border-inline-start:4px solid #25a366">%s</div>
%s
<p style="color:#999;font-size:12px;margin-top:16px">%s</p>
</body></html>`, dir,
		labels["from"], html.EscapeString(sender),
		labels["chat"], html.EscapeString(chat),
		labels["time"], tsText,
		labels["rule"], html.EscapeString(job.RuleName),
		escBody, linksHTML, labels["footer"])

	e := mail.Email{To: job.Action.Emails, Subject: subject, Text: text, HTML: h}
	if len(e.To) == 0 {
		e.To = []string{strings.TrimPrefix(job.DestinationKey, "mail:")}
	}
	if job.Action.IncludeMedia && msg.Media != nil {
		if p := x.mediaPath(msg.Media); p != "" {
			if fi, err := os.Stat(p); err == nil && fi.Size() <= MaxAttachment {
				e.Attachments = []string{p}
			} else {
				e.Text += "\n" + labels["too_large"]
				e.HTML = strings.Replace(e.HTML, "</body>", "<p>"+labels["too_large"]+"</p></body>", 1)
			}
		}
	}
	return e
}

var emailLabels = map[string]map[string]string{
	"ar": {"from": "المرسل", "chat": "المحادثة", "time": "الوقت", "rule": "القاعدة", "links": "الروابط:", "footer": "أُرسلت تلقائياً بواسطة WhatsApp Manager", "too_large": "المرفق أكبر من 20 ميجابايت ولم يُرفق."},
	"en": {"from": "From", "chat": "Chat", "time": "Time", "rule": "Rule", "links": "Links:", "footer": "Sent automatically by WhatsApp Manager", "too_large": "The attachment is larger than 20 MB and was not attached."},
}

// Header is the first line of a copied WhatsApp message.
func Header(lang string, msg *rules.Message, chatName string) string {
	sender := msg.SenderName
	if lang == "ar" {
		if msg.IsGroup() && chatName != "" {
			return fmt.Sprintf("📩 من %s في %s", sender, chatName)
		}
		return fmt.Sprintf("📩 من %s", sender)
	}
	if msg.IsGroup() && chatName != "" {
		return fmt.Sprintf("📩 From %s in %s", sender, chatName)
	}
	return fmt.Sprintf("📩 From %s", sender)
}

func mediaLabel(lang, kind string) string {
	ar := map[string]string{"image": "[صورة]", "video": "[فيديو]", "video_note": "[فيديو]", "audio": "[صوت]", "document": "[مستند]", "sticker": "[ملصق]"}
	en := map[string]string{"image": "[image]", "video": "[video]", "video_note": "[video]", "audio": "[audio]", "document": "[document]", "sticker": "[sticker]"}
	m := en
	if lang == "ar" {
		m = ar
	}
	if v, ok := m[kind]; ok {
		return v
	}
	return "[media]"
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
