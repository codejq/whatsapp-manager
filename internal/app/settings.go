package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"whatsapp-manager/internal/dispatch"
	"whatsapp-manager/internal/mail"
)

// Setting keys.
const (
	keyGeneral    = "general"
	keyRuntime    = "runtime"
	keySMTP       = "smtp"
	keySMTPPass   = "smtp_password_enc"
	keySMTPStatus = "smtp_status"
	keyLastBackup = "last_backup"
)

// General are user-facing preferences.
type General struct {
	Language            string `json:"language"` // ar | en
	Theme               string `json:"theme"`    // system | light | dark
	Autostart           bool   `json:"autostart"`
	CatchupHours        int    `json:"catchup_hours"`
	WARatePerMin        int    `json:"wa_rate_per_min"`
	EmailRatePerMin     int    `json:"email_rate_per_min"`
	Paused              bool   `json:"paused"`
	FirstRunDone        bool   `json:"first_run_done"`
	CloseHintShown      bool   `json:"close_hint_shown"`
	RetentionDays       int    `json:"retention_days"` // messages, history and media older than this are deleted
	EmailSubjectCounter bool   `json:"email_subject_counter"`
	UpdateCheck         bool   `json:"update_check"`
}

func defaultGeneral() General {
	return General{
		Language: "ar", Theme: "system", Autostart: true, CatchupHours: 12,
		WARatePerMin: 20, EmailRatePerMin: 30, RetentionDays: 5, EmailSubjectCounter: true, UpdateCheck: true,
	}
}

func (g *General) normalize() {
	if g.Language != "en" {
		g.Language = "ar"
	}
	switch g.Theme {
	case "light", "dark":
	default:
		g.Theme = "system"
	}
	g.CatchupHours = clamp(g.CatchupHours, 1, 168)
	g.WARatePerMin = clamp(g.WARatePerMin, 1, 60)
	g.EmailRatePerMin = clamp(g.EmailRatePerMin, 1, 120)
	g.RetentionDays = clamp(g.RetentionDays, 1, 365)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Runtime holds generated ports and secrets (secrets DPAPI-encrypted).
type Runtime struct {
	APIPort          int    `json:"api_port"`
	GowaPort         int    `json:"gowa_port"`
	GowaUser         string `json:"gowa_user"`
	GowaPassEnc      string `json:"gowa_pass_enc"`
	WebhookSecretEnc string `json:"webhook_secret_enc"`
}

// SMTPStatus is the last known health of the SMTP settings.
type SMTPStatus struct {
	State    string    `json:"state"` // see dispatch.SMTP*
	ErrorKey string    `json:"error_key,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	At       time.Time `json:"at"`
}

func randomSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// NewID returns a random identifier.
func NewID() string { return randomSecret(8) }

// General returns a copy of the preferences.
func (a *App) General() General {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.general
}

// SaveGeneral persists preferences and applies side effects.
func (a *App) SaveGeneral(ctx context.Context, g General) (General, error) {
	g.normalize()
	if err := a.Store.SetJSON(ctx, keyGeneral, g); err != nil {
		return g, err
	}
	a.mu.Lock()
	old := a.general
	a.general = g
	a.mu.Unlock()
	if old.Autostart != g.Autostart {
		a.applyAutostart(g.Autostart)
	}
	if old.RetentionDays != g.RetentionDays {
		go a.cleanup(context.Background()) // apply a shorter period right away
	}
	if old.Paused != g.Paused || old.Language != g.Language {
		a.notifyTray()
	}
	a.Dispatcher.Wake()
	a.Engine.Notify()
	return g, nil
}

// SMTPSettings returns settings with the decrypted password and the status.
func (a *App) SMTPSettings(ctx context.Context) (mail.Settings, SMTPStatus) {
	var s mail.Settings
	_, _ = a.Store.GetJSON(ctx, keySMTP, &s)
	if enc, ok, _ := a.Store.GetSetting(ctx, keySMTPPass); ok && enc != "" {
		if p, err := a.Secrets.Unprotect(enc); err == nil {
			s.Password = p
		} else {
			a.Log.Error("smtp password decrypt failed", "err", err)
		}
	}
	var st SMTPStatus
	if ok, _ := a.Store.GetJSON(ctx, keySMTPStatus, &st); !ok || st.State == "" {
		st.State = dispatch.SMTPNotConfigured
		if s.Configured() {
			st.State = dispatch.SMTPUntested
		}
	}
	return s, st
}

// SaveSMTP stores settings. An empty password keeps the existing one unless
// clearPassword is set. Any change resets the status to "untested".
func (a *App) SaveSMTP(ctx context.Context, s mail.Settings, clearPassword bool) error {
	s.Normalize()
	if err := s.Validate(); err != nil {
		return err
	}
	pw := s.Password
	s.Password = ""
	if err := a.Store.SetJSON(ctx, keySMTP, s); err != nil {
		return err
	}
	if pw != "" || clearPassword {
		enc, err := a.Secrets.Protect(pw)
		if err != nil {
			return err
		}
		if err := a.Store.SetSetting(ctx, keySMTPPass, enc); err != nil {
			return err
		}
	}
	return a.setSMTPStatus(ctx, SMTPStatus{State: dispatch.SMTPUntested, At: time.Now()})
}

func (a *App) setSMTPStatus(ctx context.Context, st SMTPStatus) error {
	return a.Store.SetJSON(ctx, keySMTPStatus, st)
}

// TestSMTP authenticates against the server (and optionally sends a test
// email). On success parked email jobs resume.
func (a *App) TestSMTP(ctx context.Context, sendTo string) (SMTPStatus, error) {
	s, _ := a.SMTPSettings(ctx)
	var err error
	if sendTo != "" {
		g := a.General()
		subject, body := "WhatsApp Manager — test email", "Your email settings work. WhatsApp Manager can now forward messages by email."
		if g.Language == "ar" {
			subject, body = "WhatsApp Manager — رسالة تجريبية", "إعدادات البريد تعمل بنجاح. يمكن لـ WhatsApp Manager الآن تحويل الرسائل إلى البريد الإلكتروني."
		}
		err = mail.Send(ctx, s, mail.Email{To: []string{sendTo}, Subject: subject, Text: body})
	} else {
		err = mail.Test(ctx, s)
	}
	st := SMTPStatus{State: dispatch.SMTPOK, At: time.Now()}
	if err != nil {
		key, detail := mail.Describe(err)
		st = SMTPStatus{State: dispatch.SMTPUntested, ErrorKey: key, Detail: detail, At: time.Now()}
		var me *mail.Error
		if errorsAs(mail.Classify(err), &me) && me.Kind == mail.KindAuth {
			st.State = dispatch.SMTPAuthFailed
		}
	}
	if serr := a.setSMTPStatus(ctx, st); serr != nil {
		return st, serr
	}
	if err == nil {
		if n, _ := a.Store.ResumeWaitingEmail(ctx); n > 0 {
			a.Log.Info("resumed waiting emails", "count", n)
		}
		a.Dispatcher.Wake()
	}
	return st, err
}

func (a *App) onSMTPAuthFail(err error) {
	ctx := context.Background()
	_, cur := a.SMTPSettings(ctx)
	key, detail := mail.Describe(err)
	_ = a.setSMTPStatus(ctx, SMTPStatus{State: dispatch.SMTPAuthFailed, ErrorKey: key, Detail: detail, At: time.Now()})
	if cur.State != dispatch.SMTPAuthFailed {
		a.toast("toast.smtp_auth_failed")
	}
}
