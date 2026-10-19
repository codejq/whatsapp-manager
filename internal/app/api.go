package app

import (
	"archive/zip"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"whatsapp-manager/internal/dispatch"
	"whatsapp-manager/internal/gowa"
	"whatsapp-manager/internal/mail"
	"whatsapp-manager/internal/rules"
	"whatsapp-manager/internal/store"
	"whatsapp-manager/internal/ui"
)

func errorsAs(err error, target any) bool { return errors.As(err, target) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// guard rejects requests that do not target our loopback host (DNS
// rebinding) and API calls without the per-launch token (CSRF from other pages).
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		okHost := host == fmt.Sprintf("127.0.0.1:%d", a.apiPort) || host == fmt.Sprintf("localhost:%d", a.apiPort)
		if !okHost {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+host {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			tok := r.Header.Get("X-Auth-Token")
			if tok == "" {
				tok = r.URL.Query().Get("token") // file downloads
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(a.token)) != 1 {
				writeErr(w, http.StatusUnauthorized, "errors.unauthorized", "")
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, key, detail string) {
	writeJSON(w, code, map[string]string{"error": key, "detail": detail})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 5<<20)).Decode(v)
}

// errKey maps an internal error to an i18n key + detail for the UI.
func errKey(err error) (string, string) {
	var ve *rules.ValidationError
	switch {
	case errors.As(err, &ve):
		return ve.Key, ve.Field
	case errors.Is(err, gowa.ErrNotLoggedIn):
		return "errors.wa.not_logged_in", err.Error()
	case errors.Is(err, gowa.ErrUnavailable):
		return "errors.wa.unavailable", err.Error()
	case errors.Is(err, rules.ErrNotFound):
		return "errors.not_found", ""
	}
	var me *mail.Error
	if errors.As(err, &me) {
		return me.Key, me.Err.Error()
	}
	if strings.HasPrefix(err.Error(), "errors.") {
		return err.Error(), ""
	}
	return "errors.unknown", err.Error()
}

func (a *App) fail(w http.ResponseWriter, code int, err error) {
	key, detail := errKey(err)
	writeErr(w, code, key, detail)
}

func (a *App) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/state", a.apiState)
	mux.HandleFunc("POST /api/notices/dismiss", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.notices = nil
		a.mu.Unlock()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/wa/login", a.apiLogin)
	mux.HandleFunc("POST /api/wa/pair", a.apiPair)
	mux.HandleFunc("POST /api/wa/logout", a.apiLogout)
	mux.HandleFunc("POST /api/wa/reconnect", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Super.Client().Reconnect(r.Context()); err != nil {
			a.fail(w, 502, err)
			return
		}
		a.Super.Kick()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/wa/restart", func(w http.ResponseWriter, r *http.Request) {
		a.Super.Restart()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/wa/chats", a.apiChats)
	mux.HandleFunc("POST /api/wa/check", a.apiCheck)

	mux.HandleFunc("GET /api/rules", a.apiRules)
	mux.HandleFunc("POST /api/rules", a.apiSaveRule)
	mux.HandleFunc("DELETE /api/rules/{id}", a.apiDeleteRule)
	mux.HandleFunc("POST /api/rules/{id}/enabled", a.apiEnableRule)
	mux.HandleFunc("POST /api/rules/test", a.apiTestRules)
	mux.HandleFunc("GET /api/rules/export", a.apiExportRules)
	mux.HandleFunc("POST /api/rules/import", a.apiImportRules)

	mux.HandleFunc("GET /api/activity", a.apiActivity)
	mux.HandleFunc("POST /api/outbox/{id}/retry", a.apiRetry)
	mux.HandleFunc("POST /api/outbox/{id}/cancel", a.apiCancel)

	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.General()) })
	mux.HandleFunc("PUT /api/settings", a.apiSaveSettings)

	mux.HandleFunc("GET /api/smtp", a.apiSMTP)
	mux.HandleFunc("PUT /api/smtp", a.apiSaveSMTP)
	mux.HandleFunc("POST /api/smtp/test", a.apiTestSMTP)
	mux.HandleFunc("POST /api/smtp/autodetect", a.apiAutodetectSMTP)

	mux.HandleFunc("GET /api/diagnostics", a.apiDiagnostics)
	mux.HandleFunc("POST /api/open-logs", func(w http.ResponseWriter, r *http.Request) {
		_ = a.cfg.Platform.OpenFolder(a.logDir)
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/open-url", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL string `json:"url"`
		}
		if readJSON(r, &in) != nil || !strings.HasPrefix(in.URL, "https://") {
			writeErr(w, 400, "errors.bad_request", "")
			return
		}
		_ = a.cfg.Platform.OpenURL(in.URL)
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/update", a.apiUpdate)

	// Single-page app.
	sub, err := fs.Sub(ui.FS, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			b, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(503)
				_, _ = w.Write([]byte("UI not built. Run: npm --prefix web run build"))
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
			_, _ = w.Write(b)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}

// ---------- state ----------

func (a *App) apiState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	smtpS, smtpSt := a.SMTPSettings(ctx)
	y, m, d := time.Now().Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	stats, _ := a.Store.StatsSince(ctx, midnight)
	a.mu.RLock()
	notices := append([]string{}, a.notices...)
	a.mu.RUnlock()
	rulesList, _ := a.Store.ListRules(ctx)
	enabled := 0
	for _, x := range rulesList {
		if x.Enabled {
			enabled++
		}
	}
	writeJSON(w, 200, map[string]any{
		"version":   Version,
		"channel":   Channel,
		"packaged":  a.cfg.Platform.IsPackaged(),
		"wa":        a.Super.State(),
		"smtp":      map[string]any{"state": smtpSt.State, "error_key": smtpSt.ErrorKey, "detail": smtpSt.Detail, "at": smtpSt.At, "configured": smtpS.Configured()},
		"stats":     stats,
		"general":   a.General(),
		"notices":   notices,
		"autostart": a.cfg.Platform.AutostartEnabled(),
		"rules":     map[string]int{"total": len(rulesList), "enabled": enabled},
	})
}

// ---------- WhatsApp ----------

func (a *App) apiLogin(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c := a.Super.Client()
	link, dur, err := c.LoginQR(ctx)
	if err != nil {
		var ae *gowa.APIError
		if errors.As(err, &ae) && strings.Contains(strings.ToLower(ae.Message), "already") {
			a.Super.Kick()
			writeJSON(w, 200, map[string]any{"already": true})
			return
		}
		a.fail(w, 502, err)
		return
	}
	png, err := c.FetchQR(ctx, link)
	if err != nil {
		a.fail(w, 502, err)
		return
	}
	a.Super.Kick()
	writeJSON(w, 200, map[string]any{"qr": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), "duration": dur})
}

// NormalizePhone strips formatting and returns international digits.
func NormalizePhone(p string) (string, bool) {
	var b strings.Builder
	for _, r := range p {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= '٠' && r <= '٩':
			b.WriteRune('0' + (r - '٠'))
		case r >= '۰' && r <= '۹':
			b.WriteRune('0' + (r - '۰'))
		}
	}
	d := strings.TrimPrefix(b.String(), "00")
	return d, len(d) >= 8 && len(d) <= 15
}

func (a *App) apiPair(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "errors.bad_request", "")
		return
	}
	phone, ok := NormalizePhone(in.Phone)
	if !ok {
		writeErr(w, 400, "errors.phone_invalid", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	code, err := a.Super.Client().LoginWithCode(ctx, phone)
	if err != nil {
		a.fail(w, 502, err)
		return
	}
	a.Super.Kick()
	writeJSON(w, 200, map[string]string{"code": code})
}

func (a *App) apiLogout(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := a.Super.Client().Logout(ctx); err != nil && !errors.Is(err, gowa.ErrNotLoggedIn) {
		a.fail(w, 502, err)
		return
	}
	a.mu.Lock()
	a.lastWA = gowa.WALoggedOut // a deliberate logout must not raise a toast
	a.mu.Unlock()
	a.Super.Kick()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) apiChats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var refreshErr error
	if r.URL.Query().Get("refresh") == "1" {
		refreshErr = a.refreshChats(ctx)
	}
	chats, err := a.Store.ListChats(ctx, r.URL.Query().Get("kind"))
	if err != nil {
		a.fail(w, 500, err)
		return
	}
	resp := map[string]any{"chats": chats}
	if refreshErr != nil {
		k, _ := errKey(refreshErr)
		resp["refresh_error"] = k
	}
	writeJSON(w, 200, resp)
}

func (a *App) apiCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "errors.bad_request", "")
		return
	}
	phone, ok := NormalizePhone(in.Phone)
	if !ok {
		writeErr(w, 400, "errors.phone_invalid", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	on, err := a.Super.Client().IsOnWhatsApp(ctx, phone)
	if err != nil {
		a.fail(w, 502, err)
		return
	}
	writeJSON(w, 200, map[string]any{"on_whatsapp": on, "jid": phone + "@s.whatsapp.net", "phone": phone})
}

// ---------- rules ----------

func (a *App) apiRules(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListRules(r.Context())
	if err != nil {
		a.fail(w, 500, err)
		return
	}
	if list == nil {
		list = []*rules.Rule{}
	}
	writeJSON(w, 200, list)
}

func (a *App) emailReady(ctx context.Context) bool {
	s, st := a.SMTPSettings(ctx)
	return s.Configured() && st.State == dispatch.SMTPOK
}

func (a *App) saveRule(ctx context.Context, rl *rules.Rule, keepCreated bool) error {
	rl.Normalize()
	if err := rl.Validate(); err != nil {
		return err
	}
	if rl.ID == "" {
		rl.ID = NewID()
		rl.CreatedAt = 0
	} else if keepCreated {
		if old, err := a.Store.GetRule(ctx, rl.ID); err == nil {
			rl.CreatedAt = old.CreatedAt
		} else {
			rl.CreatedAt = 0
		}
	}
	for i := range rl.Actions {
		if rl.Actions[i].ID == "" {
			rl.Actions[i].ID = NewID()
		}
	}
	if rl.Enabled && rl.HasEmailAction() && !a.emailReady(ctx) {
		return errors.New("errors.rule.smtp_not_ready")
	}
	return a.Store.SaveRule(ctx, rl)
}

func (a *App) apiSaveRule(w http.ResponseWriter, r *http.Request) {
	var rl rules.Rule
	if err := readJSON(r, &rl); err != nil {
		writeErr(w, 400, "errors.bad_request", err.Error())
		return
	}
	if err := a.saveRule(r.Context(), &rl, true); err != nil {
		a.fail(w, 400, err)
		return
	}
	writeJSON(w, 200, rl)
}

func (a *App) apiDeleteRule(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteRule(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, 404, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) apiEnableRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "errors.bad_request", "")
		return
	}
	ctx := r.Context()
	rl, err := a.Store.GetRule(ctx, r.PathValue("id"))
	if err != nil {
		a.fail(w, 404, err)
		return
	}
	if in.Enabled && rl.HasEmailAction() && !a.emailReady(ctx) {
		writeErr(w, 400, "errors.rule.smtp_not_ready", "")
		return
	}
	if err := a.Store.SetRuleEnabled(ctx, rl.ID, in.Enabled); err != nil {
		a.fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type testInput struct {
	Rule     *rules.Rule `json:"rule"`
	ChatJID  string      `json:"chat_jid"`
	ChatKind string      `json:"chat_kind"` // group | private
	Sender   string      `json:"sender"`
	Body     string      `json:"body"`
	Type     string      `json:"type"`
	FromMe   bool        `json:"from_me"`
}

func (a *App) apiTestRules(w http.ResponseWriter, r *http.Request) {
	var in testInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "errors.bad_request", "")
		return
	}
	msg := &rules.Message{ID: "TEST", Body: in.Body, Type: in.Type, IsFromMe: in.FromMe, Timestamp: time.Now(), SenderName: "Test"}
	if msg.Type == "" {
		msg.Type = "text"
	}
	msg.ChatID = in.ChatJID
	if msg.ChatID == "" {
		if in.ChatKind == "group" {
			msg.ChatID = "000000000000000000@g.us"
		} else {
			msg.ChatID = "000000000000@s.whatsapp.net"
		}
	}
	msg.From = in.Sender
	if msg.From == "" {
		if strings.HasSuffix(msg.ChatID, "@g.us") {
			msg.From = "000000000001@s.whatsapp.net"
		} else {
			msg.From = msg.ChatID
		}
	}
	var list []*rules.Rule
	if in.Rule != nil {
		rl := *in.Rule
		rl.Normalize()
		rl.Enabled = true
		list = []*rules.Rule{&rl}
	} else {
		list, _ = a.Store.ListRules(r.Context())
	}
	results := []rules.Result{}
	for _, rl := range list {
		c := *rl
		c.CreatedAt = 0
		results = append(results, rules.Evaluate(&c, msg, rules.EvalContext{Now: time.Now()}))
	}
	writeJSON(w, 200, map[string]any{"results": results, "type": msg.Type, "has_link": rules.ContainsLink(in.Body)})
}

func (a *App) apiExportRules(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListRules(r.Context())
	if err != nil {
		a.fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="whatsapp-manager-rules-%s.json"`, time.Now().Format("20060102")))
	writeJSON(w, 200, map[string]any{"app": "whatsapp-manager", "version": 1, "exported_at": time.Now(), "rules": list})
}

func (a *App) apiImportRules(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rules []*rules.Rule `json:"rules"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "errors.import_invalid", err.Error())
		return
	}
	ctx := r.Context()
	imported, disabled := 0, 0
	var firstErr string
	for _, rl := range in.Rules {
		if rl == nil {
			continue
		}
		rl.ID = ""
		rl.CreatedAt = 0 // never process history for imported rules
		for i := range rl.Actions {
			rl.Actions[i].ID = ""
		}
		if rl.Enabled && rl.HasEmailAction() && !a.emailReady(ctx) {
			rl.Enabled = false
			disabled++
		}
		if err := a.saveRule(ctx, rl, false); err != nil {
			if firstErr == "" {
				firstErr, _ = errKey(err)
			}
			continue
		}
		imported++
	}
	writeJSON(w, 200, map[string]any{"imported": imported, "disabled": disabled, "error": firstErr})
}

// ---------- activity ----------

func (a *App) apiActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	list, err := a.Store.Activity(r.Context(), store.ActivityFilter{Limit: limit, BeforeID: before, Only: q.Get("only"), Query: q.Get("q")})
	if err != nil {
		a.fail(w, 500, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *App) apiRetry(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := a.Store.RetryOutbox(r.Context(), id); err != nil {
		writeErr(w, 400, "errors.not_retryable", "")
		return
	}
	a.Dispatcher.Wake()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) apiCancel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := a.Store.CancelOutbox(r.Context(), id); err != nil {
		a.fail(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- settings ----------

func (a *App) apiSaveSettings(w http.ResponseWriter, r *http.Request) {
	g := a.General()
	if err := readJSON(r, &g); err != nil {
		writeErr(w, 400, "errors.bad_request", "")
		return
	}
	g, err := a.SaveGeneral(r.Context(), g)
	if err != nil {
		a.fail(w, 500, err)
		return
	}
	writeJSON(w, 200, g)
}

func (a *App) apiSMTP(w http.ResponseWriter, r *http.Request) {
	s, st := a.SMTPSettings(r.Context())
	has := s.Password != ""
	s.Password = ""
	s.Normalize() // fill defaults (port, security, sender name) for a fresh form
	writeJSON(w, 200, map[string]any{"settings": s, "has_password": has, "status": st, "presets": mail.Presets})
}

func (a *App) apiSaveSMTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		mail.Settings
		Password      string `json:"password"`
		ClearPassword bool   `json:"clear_password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "errors.bad_request", "")
		return
	}
	s := in.Settings
	s.Password = in.Password
	if err := a.SaveSMTP(r.Context(), s, in.ClearPassword); err != nil {
		a.fail(w, 400, err)
		return
	}
	a.apiSMTP(w, r)
}

func (a *App) apiTestSMTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	_ = readJSON(r, &in)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	st, err := a.TestSMTP(ctx, strings.TrimSpace(in.To))
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "status": st})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "status": st})
}

func (a *App) apiAutodetectSMTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "errors.bad_request", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	s, err := mail.AutoDetect(ctx, in.Email, in.Password)
	if err != nil && s.Host == "" {
		a.fail(w, 400, err)
		return
	}
	authFailed := err != nil
	if serr := a.SaveSMTP(ctx, s, false); serr != nil {
		a.fail(w, 400, serr)
		return
	}
	if !authFailed {
		_, _ = a.TestSMTP(ctx, "")
	} else {
		key, detail := mail.Describe(err)
		_ = a.setSMTPStatus(ctx, SMTPStatus{State: dispatch.SMTPAuthFailed, ErrorKey: key, Detail: detail, At: time.Now()})
	}
	a.apiSMTP(w, r)
}

// ---------- diagnostics & update ----------

func (a *App) apiDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="whatsapp-manager-diagnostics-%s.zip"`, time.Now().Format("20060102-1504")))
	zw := zip.NewWriter(w)
	defer zw.Close()
	smtpS, smtpSt := a.SMTPSettings(ctx)
	smtpS.Password = ""
	stats, _ := a.Store.StatsSince(ctx, time.Now().Add(-24*time.Hour))
	a.mu.RLock()
	rt := map[string]any{"api_port": a.apiPort, "gowa_port": a.runtime.GowaPort}
	a.mu.RUnlock()
	info := map[string]any{
		"version": Version, "channel": Channel, "packaged": a.cfg.Platform.IsPackaged(),
		"time": time.Now(), "general": a.General(), "runtime": rt,
		"wa": a.Super.State(), "smtp": smtpS, "smtp_status": smtpSt, "stats_24h": stats,
	}
	if f, err := zw.Create("info.json"); err == nil {
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		_ = enc.Encode(info)
	}
	if list, err := a.Store.ListRules(ctx); err == nil {
		if f, err := zw.Create("rules.json"); err == nil {
			_ = json.NewEncoder(f).Encode(list)
		}
	}
	logs, _ := filepath.Glob(filepath.Join(a.logDir, "*.log"))
	for _, p := range logs {
		src, err := os.Open(p)
		if err != nil {
			continue
		}
		if f, err := zw.Create("logs/" + filepath.Base(p)); err == nil {
			_, _ = io.Copy(f, src)
		}
		src.Close()
	}
}

func (a *App) apiUpdate(w http.ResponseWriter, r *http.Request) {
	res := map[string]any{"current": Version, "available": false}
	if UpdateRepo == "" || Channel != "github" || a.cfg.Platform.IsPackaged() || !a.General().UpdateCheck {
		writeJSON(w, 200, res)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+UpdateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, 200, res)
		return
	}
	defer resp.Body.Close()
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if resp.StatusCode == 200 && json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel) == nil {
		res["latest"] = rel.TagName
		res["url"] = rel.HTMLURL
		res["available"] = NewerVersion(rel.TagName, Version)
	}
	writeJSON(w, 200, res)
}

// NewerVersion reports whether semver tag a is newer than b ("v1.2.3").
func NewerVersion(a, b string) bool {
	pa, oka := parseSemver(a)
	pb, okb := parseSemver(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
