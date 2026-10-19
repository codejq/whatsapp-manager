// Package app wires the manager together: storage, the GoWA supervisor,
// the rule engine, the dispatcher, the local HTTP server and maintenance.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"whatsapp-manager/internal/dispatch"
	"whatsapp-manager/internal/engine"
	"whatsapp-manager/internal/gowa"
	"whatsapp-manager/internal/mail"
	"whatsapp-manager/internal/store"
	"whatsapp-manager/internal/webhook"
)

// Build information, set by main via ldflags.
var (
	Version    = "dev"
	UpdateRepo = "" // "owner/repo" for the GitHub update check
	Channel    = "github"
)

// Secrets encrypts values at rest (DPAPI on Windows).
type Secrets interface {
	Protect(string) (string, error)
	Unprotect(string) (string, error)
}

// Platform is the OS integration used by the app.
type Platform interface {
	Secrets
	SetAutostart(bool) error
	AutostartEnabled() bool
	IsPackaged() bool
	Toast(title, msg string)
	OpenFolder(path string) error
	OpenURL(url string) error
	Assign(pid int) error
	ProcAttr() *syscall.SysProcAttr
}

// TrayState is pushed to the tray icon.
type TrayState struct {
	WA     string // gowa.WA*
	Phase  string
	Paused bool
	Lang   string
}

// Config are startup parameters.
type Config struct {
	DataDir   string
	GowaExe   string
	Platform  Platform
	OnTray    func(TrayState)
	OnShow    func() // second instance asked to show the window
	Minimized bool
	// NoAutostart leaves the Windows autostart entry untouched (dev runs
	// with a custom --data-dir must not register themselves).
	NoAutostart bool
}

// App is the running application.
type App struct {
	cfg        Config
	Secrets    Secrets
	Store      *store.Store
	Log        *slog.Logger
	Super      *gowa.Supervisor
	Engine     *engine.Engine
	Dispatcher *dispatch.Dispatcher

	mu       sync.RWMutex
	general  General
	runtime  Runtime
	webhook  string
	token    string // per-launch UI token
	ipcToken string
	server   *http.Server
	apiPort  int
	notices  []string // one-off notices for the UI (i18n keys)
	logDir   string
	gowaDir  string
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	gu       *gowaUpdater
	waSince  time.Time
	lastWA   string
	warnedDC bool
}

// New prepares the app (opens storage, loads settings) without starting services.
func New(cfg Config) (*App, error) {
	a := &App{cfg: cfg, Secrets: cfg.Platform}
	a.logDir = filepath.Join(cfg.DataDir, "logs")
	a.gowaDir = filepath.Join(cfg.DataDir, "gowa")
	if err := os.MkdirAll(a.logDir, 0o700); err != nil {
		return nil, err
	}
	lj := &lumberjack.Logger{Filename: filepath.Join(a.logDir, "manager.log"), MaxSize: 10, MaxBackups: 5, Compress: false}
	var w io.Writer = lj
	if os.Getenv("WAM_LOG_STDERR") == "1" {
		w = io.MultiWriter(lj, os.Stderr)
	}
	a.Log = slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(a.Log)
	a.Log.Info("starting", "version", Version, "data", cfg.DataDir)

	if err := a.openStore(); err != nil {
		return nil, err
	}
	ctx := context.Background()
	a.general = defaultGeneral()
	firstRun := true
	if ok, err := a.Store.GetJSON(ctx, keyGeneral, &a.general); err == nil && ok {
		firstRun = false
	}
	a.general.normalize()
	if firstRun {
		_ = a.Store.SetJSON(ctx, keyGeneral, a.general)
	}
	if err := a.loadRuntime(ctx); err != nil {
		return nil, err
	}
	if firstRun && a.general.Autostart && !cfg.Platform.IsPackaged() {
		a.applyAutostart(true)
	}
	a.token = randomSecret(24)
	if t := os.Getenv("WAM_UI_TOKEN"); len(t) >= 16 {
		a.token = t // automated UI tests only
	}
	a.ipcToken = randomSecret(24)
	return a, nil
}

func (a *App) openStore() error {
	dbPath := filepath.Join(a.cfg.DataDir, "manager.db")
	backupDir := filepath.Join(a.cfg.DataDir, "backups")
	st, err := store.Open(dbPath)
	if err == nil {
		if ierr := st.IntegrityCheck(context.Background()); ierr == nil {
			a.Store = st
			return nil
		} else {
			err = ierr
			st.Close()
		}
	}
	// Damaged database: keep the broken file, restore the newest backup.
	a.Log.Error("database damaged; restoring backup", "err", err)
	stamp := time.Now().Format("20060102-150405")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Rename(dbPath+suffix, dbPath+suffix+".corrupt-"+stamp)
	}
	if b := store.LatestBackup(backupDir); b != "" {
		if data, rerr := os.ReadFile(b); rerr == nil {
			_ = os.WriteFile(dbPath, data, 0o600)
		}
	}
	st, err = store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	a.Store = st
	a.notices = append(a.notices, "notice.db_restored")
	return nil
}

func (a *App) loadRuntime(ctx context.Context) error {
	_, _ = a.Store.GetJSON(ctx, keyRuntime, &a.runtime)
	changed := false
	if a.runtime.GowaUser == "" {
		a.runtime.GowaUser = "wam"
		changed = true
	}
	if a.runtime.GowaPassEnc == "" {
		enc, err := a.Secrets.Protect(randomSecret(24))
		if err != nil {
			return err
		}
		a.runtime.GowaPassEnc = enc
		changed = true
	}
	if a.runtime.WebhookSecretEnc == "" {
		enc, err := a.Secrets.Protect(randomSecret(32))
		if err != nil {
			return err
		}
		a.runtime.WebhookSecretEnc = enc
		changed = true
	}
	if a.runtime.GowaPort == 0 {
		p, err := gowa.FreePort()
		if err != nil {
			return err
		}
		a.runtime.GowaPort = p
		changed = true
	}
	if changed {
		return a.Store.SetJSON(ctx, keyRuntime, a.runtime)
	}
	return nil
}

func (a *App) secret(enc string) string {
	v, err := a.Secrets.Unprotect(enc)
	if err != nil {
		a.Log.Error("secret decrypt failed", "err", err)
	}
	return v
}

// UIURL is the address the window opens (includes the per-launch token).
func (a *App) UIURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/#/?t=%s", a.apiPort, a.token)
}

// Start launches all services.
func (a *App) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	if n, err := a.Store.RecoverInFlight(ctx); err == nil && n > 0 {
		a.Log.Warn("jobs interrupted by previous shutdown marked uncertain", "count", n)
		a.notices = append(a.notices, "notice.uncertain_jobs")
	}

	a.Engine = engine.New(a.Store, a.engineOptions, a.Log, func() { a.Dispatcher.Wake() })
	gowaExe := a.initGowaUpdater(ctx)

	if err := a.startServer(ctx); err != nil {
		return err
	}
	a.webhook = fmt.Sprintf("http://127.0.0.1:%d/hook", a.apiPort)

	gowaLog := &lumberjack.Logger{Filename: filepath.Join(a.logDir, "gowa.log"), MaxSize: 10, MaxBackups: 3}
	a.Super = gowa.NewSupervisor(gowa.Config{
		ExePath:       gowaExe,
		FallbackExe:   a.cfg.GowaExe,
		OnFallback:    a.onGowaFallback,
		WorkDir:       a.gowaDir,
		Port:          a.runtime.GowaPort,
		User:          a.runtime.GowaUser,
		Pass:          a.secret(a.runtime.GowaPassEnc),
		WebhookURL:    a.webhook,
		WebhookSecret: a.secret(a.runtime.WebhookSecretEnc),
		Log:           gowaLog,
		Logger:        a.Log.With("component", "gowa"),
		ProcAttr:      procAttr(a.cfg.Platform),
		Assign:        a.cfg.Platform.Assign,
		OnPortChange: func(p int) {
			a.mu.Lock()
			a.runtime.GowaPort = p
			rt := a.runtime
			a.mu.Unlock()
			_ = a.Store.SetJSON(context.Background(), keyRuntime, rt)
		},
	})
	a.Super.Subscribe(a.onGowaState)

	a.Dispatcher = dispatch.New(dispatch.Deps{
		Store:       a.Store,
		WA:          func() dispatch.WhatsApp { return a.Super.Client() },
		WAConnected: func() bool { return a.Super.State().WA == gowa.WAConnected },
		SMTP: func() (mail.Settings, string) {
			s, st := a.SMTPSettings(context.Background())
			return s, st.State
		},
		OnAuthFail:     a.onSMTPAuthFail,
		GowaDir:        a.gowaDir,
		Lang:           func() string { return a.General().Language },
		WARate:         func() int { return a.General().WARatePerMin },
		EmailRate:      func() int { return a.General().EmailRatePerMin },
		SubjectCounter: func() bool { return a.General().EmailSubjectCounter },
		Log:            a.Log.With("component", "dispatch"),
	})

	a.goSafe("supervisor", func() { a.Super.Run(ctx) })
	a.goSafe("engine", func() { a.Engine.Run(ctx) })
	a.goSafe("dispatcher", func() { a.Dispatcher.Run(ctx) })
	a.goSafe("maintenance", func() { a.maintenance(ctx) })
	a.writeInstanceFile()
	a.notifyTray()
	return nil
}

// goSafe runs fn in a goroutine and restarts it if it panics.
func (a *App) goSafe(name string, fn func()) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			panicked := func() (p bool) {
				defer func() {
					if r := recover(); r != nil {
						a.Log.Error("goroutine panic; restarting", "name", name, "panic", r)
						p = true
					}
				}()
				fn()
				return false
			}()
			if !panicked {
				return
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

func (a *App) engineOptions() engine.Options {
	g := a.General()
	return engine.Options{Paused: g.Paused, MaxAge: time.Duration(g.CatchupHours) * time.Hour}
}

func (a *App) startServer(ctx context.Context) error {
	ln, err := listenPreferred(a.runtime.APIPort)
	if err != nil {
		return err
	}
	a.apiPort = ln.Addr().(*net.TCPAddr).Port
	if a.apiPort != a.runtime.APIPort {
		a.runtime.APIPort = a.apiPort
		_ = a.Store.SetJSON(ctx, keyRuntime, a.runtime)
	}
	mux := http.NewServeMux()
	whSecret := a.secret(a.runtime.WebhookSecretEnc)
	mux.Handle("/hook", webhook.Handler(a.Store, func() string { return whSecret }, func() { a.Engine.Notify() }, a.Log.With("component", "webhook")))
	mux.HandleFunc("/ipc/show", a.handleIPCShow)
	a.registerAPI(mux)
	a.server = &http.Server{Handler: a.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := a.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.Log.Error("http server stopped", "err", err)
		}
	}()
	a.Log.Info("http server listening", "port", a.apiPort)
	return nil
}

func listenPreferred(port int) (net.Listener, error) {
	if port > 0 {
		if l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
			return l, nil
		}
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

// Stop shuts everything down gracefully.
func (a *App) Stop() {
	a.Log.Info("stopping")
	if a.cancel != nil {
		a.cancel()
	}
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		a.Log.Warn("shutdown timed out")
	}
	if a.server != nil {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = a.server.Shutdown(sctx)
		cancel()
	}
	_ = os.Remove(a.instanceFile())
	_ = a.Store.Close()
}

// ---------- state & tray ----------

func (a *App) onGowaState(st gowa.State) {
	a.mu.Lock()
	prev := a.lastWA
	a.lastWA = st.WA
	if st.WA != gowa.WADisconnected {
		a.warnedDC = false
	}
	a.mu.Unlock()
	a.Log.Info("whatsapp state", "phase", st.Phase, "wa", st.WA)
	if st.Phase == gowa.PhaseRunning && st.WA == gowa.WAUnknown {
		go a.refreshGowaVersion(context.Background())
	}
	if st.WA == gowa.WAConnected && prev != gowa.WAConnected {
		go a.refreshChats(context.Background())
		a.Dispatcher.Wake()
	}
	if st.WA == gowa.WALoggedOut && prev == gowa.WAConnected {
		a.toast("toast.logged_out")
	}
	if st.WA == gowa.WADisconnected && prev == gowa.WAConnected {
		go a.warnIfStillDisconnected()
	}
	a.notifyTray()
}

func (a *App) warnIfStillDisconnected() {
	time.Sleep(5 * time.Minute)
	if a.Super.State().WA == gowa.WADisconnected {
		a.mu.Lock()
		warn := !a.warnedDC
		a.warnedDC = true
		a.mu.Unlock()
		if warn {
			a.toast("toast.disconnected")
		}
	}
}

func (a *App) notifyTray() {
	if a.cfg.OnTray == nil {
		return
	}
	st := gowa.State{Phase: gowa.PhaseStarting, WA: gowa.WAUnknown}
	if a.Super != nil {
		st = a.Super.State()
	}
	g := a.General()
	a.cfg.OnTray(TrayState{WA: st.WA, Phase: st.Phase, Paused: g.Paused, Lang: g.Language})
}

// SetPaused toggles all rules (tray menu).
func (a *App) SetPaused(p bool) {
	g := a.General()
	g.Paused = p
	_, _ = a.SaveGeneral(context.Background(), g)
}

// SetLanguage switches the UI language (tray menu).
func (a *App) SetLanguage(lang string) {
	g := a.General()
	g.Language = lang
	_, _ = a.SaveGeneral(context.Background(), g)
}

func (a *App) toast(key string) {
	lang := a.General().Language
	t, ok := toastText[lang][key]
	if !ok {
		t = toastText["en"][key]
	}
	a.cfg.Platform.Toast(t[0], t[1])
}

// CloseHint shows the "still running in the tray" toast the first time the window closes.
func (a *App) CloseHint() {
	g := a.General()
	if g.CloseHintShown {
		return
	}
	g.CloseHintShown = true
	_, _ = a.SaveGeneral(context.Background(), g)
	a.toast("toast.still_running")
}

var toastText = map[string]map[string][2]string{
	"ar": {
		"toast.logged_out":       {"تم تسجيل الخروج من واتساب", "افتح WhatsApp Manager وامسح رمز QR لإعادة الربط."},
		"toast.disconnected":     {"واتساب غير متصل", "انقطع الاتصال منذ 5 دقائق. تحقق من الإنترنت أو من هاتفك."},
		"toast.smtp_auth_failed": {"فشل تسجيل الدخول للبريد", "تحقق من إعدادات البريد. الرسائل محفوظة وستُرسل بعد الإصلاح."},
		"toast.still_running":    {"WhatsApp Manager ما زال يعمل", "التطبيق يعمل في شريط المهام. للإغلاق اختر «خروج» من أيقونة الشريط."},
		"toast.gowa_update":      {"تحديث جديد لخدمة واتساب", "يتوفر إصدار جديد من خدمة واتساب. افتح الإعدادات ← واتساب لتنزيله."},
	},
	"en": {
		"toast.logged_out":       {"WhatsApp logged out", "Open WhatsApp Manager and scan the QR code to reconnect."},
		"toast.disconnected":     {"WhatsApp disconnected", "Offline for 5 minutes. Check the internet connection or your phone."},
		"toast.smtp_auth_failed": {"Email login failed", "Check the email settings. Messages are kept and will be sent once fixed."},
		"toast.still_running":    {"WhatsApp Manager is still running", "It keeps working in the system tray. Use Quit in the tray menu to exit."},
		"toast.gowa_update":      {"WhatsApp service update", "A new version of the WhatsApp service is available. Open Settings → WhatsApp to download it."},
	},
}

func (a *App) applyAutostart(on bool) {
	if a.cfg.Platform.IsPackaged() || a.cfg.NoAutostart {
		return
	}
	if err := a.cfg.Platform.SetAutostart(on); err != nil {
		a.Log.Warn("autostart change failed", "err", err)
	}
}

// refreshChats updates the cached group/contact names.
func (a *App) refreshChats(ctx context.Context) error {
	c := a.Super.Client()
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var all []store.Chat
	groups, gerr := c.Groups(cctx)
	for _, g := range groups {
		all = append(all, store.Chat{JID: g.JID, Name: g.Name, Kind: "group"})
	}
	contacts, cerr := c.Contacts(cctx)
	for _, x := range contacts {
		all = append(all, store.Chat{JID: x.JID, Name: x.Name, Kind: "contact"})
	}
	if len(all) > 0 {
		if err := a.Store.UpsertChats(ctx, all); err != nil {
			return err
		}
	}
	if gerr != nil {
		return gerr
	}
	return cerr
}

// ---------- second instance ----------

type instanceInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func (a *App) instanceFile() string { return filepath.Join(a.cfg.DataDir, "instance.json") }

func (a *App) writeInstanceFile() {
	b := fmt.Sprintf(`{"port":%d,"token":%q}`, a.apiPort, a.ipcToken)
	_ = os.WriteFile(a.instanceFile(), []byte(b), 0o600)
}

func (a *App) handleIPCShow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("X-IPC-Token") != a.ipcToken {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if a.cfg.OnShow != nil {
		go a.cfg.OnShow()
	}
	w.WriteHeader(http.StatusNoContent)
}

// SignalRunningInstance asks an already-running instance to show its window.
func SignalRunningInstance(dataDir string) error {
	b, err := os.ReadFile(filepath.Join(dataDir, "instance.json"))
	if err != nil {
		return err
	}
	var info instanceInfo
	if err := jsonUnmarshal(b, &info); err != nil {
		return err
	}
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/ipc/show", info.Port), nil)
	req.Header.Set("X-IPC-Token", info.Token)
	cl := &http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("ipc status %d", resp.StatusCode)
	}
	return nil
}
