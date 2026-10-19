// Package mail sends email through the user's own SMTP server.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	"os"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

// Security modes.
const (
	SecSTARTTLS = "starttls"
	SecSSL      = "ssl"
	SecNone     = "none"
)

// Auth modes.
const (
	AuthPlain   = "plain"
	AuthLogin   = "login"
	AuthCRAMMD5 = "crammd5"
	AuthNone    = "none"
)

// Settings are the SMTP settings. Password is never serialized to the UI.
type Settings struct {
	Preset            string `json:"preset"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Security          string `json:"security"`
	Auth              string `json:"auth"`
	Username          string `json:"username"`
	Password          string `json:"-"`
	FromAddress       string `json:"from_address"`
	FromName          string `json:"from_name"`
	ReplyTo           string `json:"reply_to"`
	SkipVerify        bool   `json:"skip_verify"`
	AllowInsecureAuth bool   `json:"allow_insecure_auth"`
	TimeoutSec        int    `json:"timeout_sec"`
}

// Normalize fills defaults.
func (s *Settings) Normalize() {
	s.Host = strings.TrimSpace(s.Host)
	s.Username = strings.TrimSpace(s.Username)
	s.FromAddress = strings.TrimSpace(s.FromAddress)
	s.FromName = strings.TrimSpace(s.FromName)
	s.ReplyTo = strings.TrimSpace(s.ReplyTo)
	if s.Security == "" {
		s.Security = SecSTARTTLS
	}
	if s.Auth == "" {
		s.Auth = AuthPlain
	}
	if s.Port == 0 {
		switch s.Security {
		case SecSSL:
			s.Port = 465
		case SecNone:
			s.Port = 25
		default:
			s.Port = 587
		}
	}
	if s.TimeoutSec <= 0 {
		s.TimeoutSec = 30
	}
	if s.Username == "" && s.Auth != AuthNone {
		s.Username = s.FromAddress
	}
	if s.FromName == "" {
		s.FromName = "WhatsApp Manager"
	}
}

// Validate returns an i18n key for the first problem, or nil.
func (s *Settings) Validate() error {
	if s.Host == "" {
		return errors.New("errors.smtp.host_required")
	}
	if s.Port < 1 || s.Port > 65535 {
		return errors.New("errors.smtp.port_invalid")
	}
	if _, err := netmail.ParseAddress(s.FromAddress); err != nil {
		return errors.New("errors.smtp.from_invalid")
	}
	if s.ReplyTo != "" {
		if _, err := netmail.ParseAddress(s.ReplyTo); err != nil {
			return errors.New("errors.smtp.reply_to_invalid")
		}
	}
	switch s.Security {
	case SecSTARTTLS, SecSSL, SecNone:
	default:
		return errors.New("errors.smtp.security_invalid")
	}
	switch s.Auth {
	case AuthPlain, AuthLogin, AuthCRAMMD5, AuthNone:
	default:
		return errors.New("errors.smtp.auth_invalid")
	}
	if s.Security == SecNone && s.Auth != AuthNone && !s.AllowInsecureAuth {
		return errors.New("errors.smtp.insecure_auth")
	}
	return nil
}

// Configured reports whether enough is filled in to try sending.
func (s *Settings) Configured() bool { return s.Host != "" && s.FromAddress != "" }

// Preset is a provider template.
type Preset struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
	HelpURL  string `json:"help_url,omitempty"`
}

// Presets only pre-fill the form; every field stays editable.
var Presets = []Preset{
	{ID: "gmail", Name: "Gmail", Host: "smtp.gmail.com", Port: 587, Security: SecSTARTTLS, HelpURL: "https://support.google.com/accounts/answer/185833"},
	{ID: "outlook", Name: "Outlook.com / Hotmail", Host: "smtp-mail.outlook.com", Port: 587, Security: SecSTARTTLS, HelpURL: "https://support.microsoft.com/account-billing/how-to-get-and-use-app-passwords-5896ed9b-4263-e681-128a-a6f2979a7944"},
	{ID: "office365", Name: "Microsoft 365", Host: "smtp.office365.com", Port: 587, Security: SecSTARTTLS},
	{ID: "yahoo", Name: "Yahoo", Host: "smtp.mail.yahoo.com", Port: 465, Security: SecSSL, HelpURL: "https://help.yahoo.com/kb/SLN15241.html"},
	{ID: "zoho", Name: "Zoho", Host: "smtp.zoho.com", Port: 465, Security: SecSSL},
	{ID: "icloud", Name: "iCloud", Host: "smtp.mail.me.com", Port: 587, Security: SecSTARTTLS, HelpURL: "https://support.apple.com/102654"},
	{ID: "custom", Name: "Custom", Port: 587, Security: SecSTARTTLS},
}

var domainPresets = map[string]string{
	"gmail.com": "gmail", "googlemail.com": "gmail",
	"outlook.com": "outlook", "hotmail.com": "outlook", "live.com": "outlook", "msn.com": "outlook",
	"yahoo.com": "yahoo", "ymail.com": "yahoo",
	"zoho.com": "zoho", "zohomail.com": "zoho",
	"icloud.com": "icloud", "me.com": "icloud", "mac.com": "icloud",
}

// PresetByID finds a preset.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Email is a message to send.
type Email struct {
	To          []string
	Subject     string
	Text        string
	HTML        string
	Attachments []string
}

func (s *Settings) client() (*gomail.Client, error) {
	opts := []gomail.Option{
		gomail.WithPort(s.Port),
		gomail.WithTimeout(time.Duration(s.TimeoutSec) * time.Second),
		gomail.WithTLSConfig(&tls.Config{ServerName: s.Host, InsecureSkipVerify: s.SkipVerify, MinVersion: tls.VersionTLS12}), //nolint:gosec // user opt-in for self-signed servers
	}
	switch s.Security {
	case SecSSL:
		opts = append(opts, gomail.WithSSL())
	case SecNone:
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	default:
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	}
	switch s.Auth {
	case AuthNone:
	case AuthLogin:
		t := gomail.SMTPAuthLogin
		if s.Security == SecNone {
			t = gomail.SMTPAuthLoginNoEnc
		}
		opts = append(opts, gomail.WithSMTPAuth(t), gomail.WithUsername(s.Username), gomail.WithPassword(s.Password))
	case AuthCRAMMD5:
		opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthCramMD5), gomail.WithUsername(s.Username), gomail.WithPassword(s.Password))
	default:
		t := gomail.SMTPAuthPlain
		if s.Security == SecNone {
			t = gomail.SMTPAuthPlainNoEnc
		}
		opts = append(opts, gomail.WithSMTPAuth(t), gomail.WithUsername(s.Username), gomail.WithPassword(s.Password))
	}
	return gomail.NewClient(s.Host, opts...)
}

// Send delivers one email.
func Send(ctx context.Context, s Settings, e Email) error {
	s.Normalize()
	if err := s.Validate(); err != nil {
		return &Error{Kind: KindConfig, Key: err.Error(), Err: err}
	}
	m := gomail.NewMsg()
	if err := m.FromFormat(s.FromName, s.FromAddress); err != nil {
		return &Error{Kind: KindConfig, Key: "errors.smtp.from_invalid", Err: err}
	}
	if err := m.To(e.To...); err != nil {
		return &Error{Kind: KindRecipient, Key: "errors.smtp.recipient_rejected", Err: err}
	}
	if s.ReplyTo != "" {
		_ = m.ReplyTo(s.ReplyTo)
	}
	m.Subject(e.Subject)
	m.SetMessageID()
	m.SetDate()
	m.SetBodyString(gomail.TypeTextPlain, e.Text)
	if e.HTML != "" {
		m.AddAlternativeString(gomail.TypeTextHTML, e.HTML)
	}
	for _, a := range e.Attachments {
		if _, err := os.Stat(a); err == nil {
			m.AttachFile(a)
		}
	}
	c, err := s.client()
	if err != nil {
		return Classify(err)
	}
	if err := c.DialAndSendWithContext(ctx, m); err != nil {
		return Classify(err)
	}
	return nil
}

// Test connects and authenticates without sending.
func Test(ctx context.Context, s Settings) error {
	s.Normalize()
	if err := s.Validate(); err != nil {
		return &Error{Kind: KindConfig, Key: err.Error(), Err: err}
	}
	c, err := s.client()
	if err != nil {
		return Classify(err)
	}
	if err := c.DialWithContext(ctx); err != nil {
		return Classify(err)
	}
	return c.Close()
}

// AutoDetect tries known presets and common host names for the address and
// returns the first settings that connect and authenticate.
func AutoDetect(ctx context.Context, email, password string) (Settings, error) {
	addr, err := netmail.ParseAddress(strings.TrimSpace(email))
	if err != nil {
		return Settings{}, &Error{Kind: KindConfig, Key: "errors.smtp.from_invalid", Err: err}
	}
	domain := strings.ToLower(addr.Address[strings.LastIndex(addr.Address, "@")+1:])
	base := Settings{FromAddress: addr.Address, Username: addr.Address, Password: password, Auth: AuthPlain}
	var candidates []Settings
	if id, ok := domainPresets[domain]; ok {
		p, _ := PresetByID(id)
		c := base
		c.Preset, c.Host, c.Port, c.Security = p.ID, p.Host, p.Port, p.Security
		candidates = append(candidates, c)
	}
	for _, h := range []string{"smtp." + domain, "mail." + domain} {
		for _, ps := range []struct {
			port int
			sec  string
		}{{587, SecSTARTTLS}, {465, SecSSL}} {
			c := base
			c.Preset, c.Host, c.Port, c.Security = "custom", h, ps.port, ps.sec
			candidates = append(candidates, c)
		}
	}
	var lastErr error
	for _, c := range candidates {
		c.TimeoutSec = 10
		tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := Test(tctx, c)
		cancel()
		if err == nil {
			c.TimeoutSec = 30
			return c, nil
		}
		lastErr = err
		var me *Error
		if errors.As(err, &me) && me.Kind == KindAuth {
			// The server exists and the password is wrong: stop here.
			c.TimeoutSec = 30
			return c, err
		}
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no candidates")
	}
	return Settings{}, &Error{Kind: KindNetwork, Key: "errors.smtp.autodetect_failed", Err: lastErr}
}

// Error kinds.
const (
	KindConfig    = "config"
	KindNetwork   = "network"
	KindTLS       = "tls"
	KindAuth      = "auth"
	KindSender    = "sender"
	KindRecipient = "recipient"
	KindTemporary = "temporary"
	KindOther     = "other"
)

// Error is a classified SMTP failure with an i18n key.
type Error struct {
	Kind string
	Key  string
	Err  error
}

func (e *Error) Error() string { return e.Key + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Transient reports whether retrying later may succeed.
func (e *Error) Transient() bool {
	return e.Kind == KindNetwork || e.Kind == KindTemporary
}

// Classify maps a go-mail/network error to an Error.
func Classify(err error) error {
	if err == nil {
		return nil
	}
	var me *Error
	if errors.As(err, &me) {
		return me
	}
	msg := strings.ToLower(err.Error())
	mk := func(kind, key string) error { return &Error{Kind: kind, Key: key, Err: err} }
	switch {
	case strings.Contains(msg, "535") || strings.Contains(msg, "534") || strings.Contains(msg, "530 5.7") ||
		strings.Contains(msg, "authentication failed") || strings.Contains(msg, "username and password not accepted") ||
		strings.Contains(msg, "invalid credentials") || strings.Contains(msg, "auth failed") || strings.Contains(msg, "authentication unsuccessful"):
		return mk(KindAuth, "errors.smtp.auth_failed")
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return mk(KindTLS, "errors.smtp.certificate")
	case strings.Contains(msg, "tls") && (strings.Contains(msg, "handshake") || strings.Contains(msg, "not supported") || strings.Contains(msg, "starttls")):
		return mk(KindTLS, "errors.smtp.tls_failed")
	case strings.Contains(msg, "mail from") || strings.Contains(msg, "sender address rejected") || strings.Contains(msg, "553"):
		return mk(KindSender, "errors.smtp.sender_rejected")
	case strings.Contains(msg, "rcpt to") || strings.Contains(msg, "recipient") || strings.Contains(msg, "550") || strings.Contains(msg, "mailbox unavailable"):
		return mk(KindRecipient, "errors.smtp.recipient_rejected")
	}
	var se *gomail.SendError
	if errors.As(err, &se) && se.IsTemp() {
		return mk(KindTemporary, "errors.smtp.temporary")
	}
	var ne net.Error
	var oe *net.OpError
	var de *net.DNSError
	if errors.As(err, &de) || errors.As(err, &oe) || errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") || strings.Contains(msg, "timeout") || strings.Contains(msg, "eof") {
		return mk(KindNetwork, "errors.smtp.unreachable")
	}
	for _, code := range []string{" 421", " 450", " 451", " 452", "4.7.0", "4.3.0"} {
		if strings.Contains(msg, code) {
			return mk(KindTemporary, "errors.smtp.temporary")
		}
	}
	return mk(KindOther, "errors.smtp.unknown")
}

// Describe returns "key" and a raw detail string for UI display.
func Describe(err error) (key, detail string) {
	var me *Error
	if errors.As(Classify(err), &me) {
		return me.Key, me.Err.Error()
	}
	return "errors.smtp.unknown", fmt.Sprint(err)
}
