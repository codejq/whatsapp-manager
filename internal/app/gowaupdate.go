package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"whatsapp-manager/internal/gowa"
	"whatsapp-manager/internal/semver"
)

// Setting keys for GoWA updates.
const (
	keyGowaOverride  = "gowa_override" // {version, exe} of a downloaded GoWA in use
	keyGowaBad       = "gowa_bad"      // versions that failed to start
	keyGowaChecked   = "gowa_checked"  // time of the last automatic check
	keyGowaNotified  = "gowa_notified" // last version a toast was shown for
	gowaCheckEvery   = 24 * time.Hour
	gowaStartTimeout = 90 * time.Second
)

// Update states.
const (
	GUIdle        = "idle"
	GUChecking    = "checking"
	GUDownloading = "downloading"
	GUInstalling  = "installing"
	GUFailed      = "failed"
	GUDone        = "done"
)

type gowaOverride struct {
	Version string `json:"version"`
	Exe     string `json:"exe"`
}

// GowaUpdate is the update status shown in the UI.
type GowaUpdate struct {
	Allowed    bool      `json:"allowed"`    // false in the Store build
	Current    string    `json:"current"`    // running GoWA version
	Bundled    bool      `json:"bundled"`    // running the version shipped with the app
	Latest     string    `json:"latest"`     // newest release found
	PageURL    string    `json:"page_url"`   // release notes
	Available  bool      `json:"available"`  // newer and not known-bad
	Compatible bool      `json:"compatible"` // same major version (safe to install)
	State      string    `json:"state"`
	Progress   int       `json:"progress"` // 0-100 while downloading
	Error      string    `json:"error,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

type gowaUpdater struct {
	mu      sync.Mutex
	u       *gowa.Updater
	bundled string
	status  GowaUpdate
	release gowa.Release
}

func (a *App) gowaUpdateAllowed() bool {
	return Channel != "store" && !a.cfg.Platform.IsPackaged()
}

// initGowaUpdater picks the GoWA binary to start with: a previously
// installed update if it still exists, otherwise the bundled one.
func (a *App) initGowaUpdater(ctx context.Context) (exe string) {
	a.gu = &gowaUpdater{
		u:       gowa.NewUpdater(filepath.Join(a.cfg.DataDir, "gowa-bin")),
		bundled: a.cfg.GowaExe,
		status:  GowaUpdate{State: GUIdle, Allowed: a.gowaUpdateAllowed()},
	}
	exe = a.cfg.GowaExe
	var ov gowaOverride
	if ok, _ := a.Store.GetJSON(ctx, keyGowaOverride, &ov); ok && ov.Exe != "" && a.gowaUpdateAllowed() {
		if _, err := os.Stat(ov.Exe); err == nil {
			a.Log.Info("using updated gowa", "version", ov.Version, "exe", ov.Exe)
			exe = ov.Exe
		}
	}
	return exe
}

// onGowaFallback is called by the supervisor when a downloaded GoWA does not start.
func (a *App) onGowaFallback(bad string) {
	ctx := context.Background()
	var ov gowaOverride
	_, _ = a.Store.GetJSON(ctx, keyGowaOverride, &ov)
	if ov.Exe == bad && ov.Version != "" {
		a.markGowaBad(ctx, ov.Version)
	}
	_ = a.Store.SetSetting(ctx, keyGowaOverride, "{}")
	a.mu.Lock()
	a.notices = append(a.notices, "notice.gowa_rolled_back")
	a.mu.Unlock()
}

func (a *App) markGowaBad(ctx context.Context, version string) {
	var bad []string
	_, _ = a.Store.GetJSON(ctx, keyGowaBad, &bad)
	for _, b := range bad {
		if b == version {
			return
		}
	}
	_ = a.Store.SetJSON(ctx, keyGowaBad, append(bad, version))
}

func (a *App) gowaIsBad(ctx context.Context, version string) bool {
	var bad []string
	_, _ = a.Store.GetJSON(ctx, keyGowaBad, &bad)
	for _, b := range bad {
		if b == version {
			return true
		}
	}
	return false
}

// refreshGowaVersion records the version of the running GoWA.
func (a *App) refreshGowaVersion(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	v, err := a.Super.Client().Version(cctx)
	if err != nil {
		return
	}
	a.gu.mu.Lock()
	a.gu.status.Current = semver.Canonical(v)
	a.gu.status.Bundled = filepath.Clean(a.Super.Exe()) == filepath.Clean(a.gu.bundled)
	a.gu.recompute(a, ctx)
	a.gu.mu.Unlock()
}

// recompute derives Available/Compatible. Caller holds g.mu.
func (g *gowaUpdater) recompute(a *App, ctx context.Context) {
	s := &g.status
	s.Available = s.Latest != "" && s.Current != "" && semver.Newer(s.Latest, s.Current) && !a.gowaIsBad(ctx, s.Latest)
	s.Compatible = s.Available && semver.SameMajor(s.Latest, s.Current)
}

// GowaStatus returns the current update status.
func (a *App) GowaStatus() GowaUpdate {
	a.gu.mu.Lock()
	defer a.gu.mu.Unlock()
	return a.gu.status
}

// CheckGowaUpdate asks GitHub for the newest GoWA release.
func (a *App) CheckGowaUpdate(ctx context.Context) (GowaUpdate, error) {
	if !a.gowaUpdateAllowed() {
		return a.GowaStatus(), nil
	}
	a.gu.mu.Lock()
	busy := a.gu.status.State == GUDownloading || a.gu.status.State == GUInstalling
	a.gu.mu.Unlock()
	if busy {
		return a.GowaStatus(), nil
	}
	rel, err := a.gu.u.Latest(ctx)
	_ = a.Store.SetJSON(ctx, keyGowaChecked, time.Now())
	a.gu.mu.Lock()
	a.gu.status.CheckedAt = time.Now()
	if err != nil {
		a.gu.mu.Unlock()
		a.Log.Warn("gowa update check failed", "err", err)
		return a.GowaStatus(), err
	}
	a.gu.release = rel
	a.gu.status.Latest = rel.Version
	a.gu.status.PageURL = rel.PageURL
	if a.gu.status.State == GUFailed || a.gu.status.State == GUDone {
		a.gu.status.State = GUIdle
	}
	a.gu.recompute(a, ctx)
	st := a.gu.status
	a.gu.mu.Unlock()

	if st.Available && st.Compatible {
		var notified string
		_, _ = a.Store.GetJSON(ctx, keyGowaNotified, &notified)
		if notified != st.Latest {
			_ = a.Store.SetJSON(ctx, keyGowaNotified, st.Latest)
			a.toast("toast.gowa_update")
		}
	}
	return st, nil
}

// autoCheckGowa runs the daily check (from maintenance).
func (a *App) autoCheckGowa(ctx context.Context) {
	if !a.gowaUpdateAllowed() || !a.General().GowaUpdateCheck {
		return
	}
	var last time.Time
	_, _ = a.Store.GetJSON(ctx, keyGowaChecked, &last)
	if time.Since(last) < gowaCheckEvery && a.GowaStatus().Latest != "" {
		return
	}
	_, _ = a.CheckGowaUpdate(ctx)
}

// errGowaBusy is returned when an install is already running.
var errGowaBusy = errors.New("errors.gowa_update_busy")

// InstallGowaUpdate downloads, verifies and switches to the latest GoWA in
// the background. If the new version does not start, the previous one is
// restored automatically.
func (a *App) InstallGowaUpdate() error {
	if !a.gowaUpdateAllowed() {
		return errors.New("errors.gowa_update_not_allowed")
	}
	a.gu.mu.Lock()
	if a.gu.status.State == GUDownloading || a.gu.status.State == GUInstalling {
		a.gu.mu.Unlock()
		return errGowaBusy
	}
	if !a.gu.status.Available || !a.gu.status.Compatible || a.gu.release.AssetURL == "" {
		a.gu.mu.Unlock()
		return errors.New("errors.gowa_update_none")
	}
	rel := a.gu.release
	a.gu.status.State, a.gu.status.Progress, a.gu.status.Error, a.gu.status.Detail = GUDownloading, 0, "", ""
	a.gu.mu.Unlock()
	go a.installGowa(rel)
	return nil
}

func (a *App) setGU(fn func(*GowaUpdate)) {
	a.gu.mu.Lock()
	fn(&a.gu.status)
	a.gu.mu.Unlock()
}

func (a *App) installGowa(rel gowa.Release) {
	ctx := context.Background()
	fail := func(key string, err error) {
		a.Log.Error("gowa update failed", "version", rel.Version, "err", err)
		a.setGU(func(s *GowaUpdate) {
			s.State, s.Error = GUFailed, key
			if err != nil {
				s.Detail = err.Error()
			}
		})
	}
	defer func() {
		if r := recover(); r != nil {
			fail("errors.gowa_update_failed", errors.New("panic"))
		}
	}()

	a.Log.Info("downloading gowa update", "version", rel.Version)
	exe, err := a.gu.u.Install(ctx, rel, func(done, total int64) {
		if total <= 0 {
			total = rel.Size
		}
		if total > 0 {
			p := int(done * 100 / total)
			a.setGU(func(s *GowaUpdate) { s.Progress = p })
		}
	})
	if err != nil {
		fail("errors.gowa_download_failed", err)
		return
	}
	a.setGU(func(s *GowaUpdate) { s.State, s.Progress = GUInstalling, 100 })

	// Do not interrupt a WhatsApp send in progress.
	for i := 0; i < 30; i++ {
		if n, _ := a.Store.CountSending(ctx, "whatsapp"); n == 0 {
			break
		}
		time.Sleep(time.Second)
	}

	prev := a.Super.Exe()
	a.Super.SetExe(exe)
	a.Super.Restart()
	if a.waitGowaVersion(ctx, rel.Version) {
		_ = a.Store.SetJSON(ctx, keyGowaOverride, gowaOverride{Version: rel.Version, Exe: exe})
		a.gu.u.Prune(exe)
		a.Log.Info("gowa updated", "version", rel.Version)
		a.setGU(func(s *GowaUpdate) { s.State = GUDone })
		a.refreshGowaVersion(ctx)
		return
	}
	// Roll back.
	a.Log.Error("new gowa did not start; rolling back", "version", rel.Version)
	a.markGowaBad(ctx, rel.Version)
	a.Super.SetExe(prev)
	a.Super.Restart()
	fail("errors.gowa_update_failed", nil)
	a.waitGowaVersion(ctx, "")
	a.refreshGowaVersion(ctx)
}

// waitGowaVersion waits until GoWA is running (and reports want, if set).
func (a *App) waitGowaVersion(ctx context.Context, want string) bool {
	deadline := time.Now().Add(gowaStartTimeout)
	time.Sleep(2 * time.Second) // let the restart begin
	for time.Now().Before(deadline) {
		if a.Super.State().Phase == gowa.PhaseRunning {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			v, err := a.Super.Client().Version(cctx)
			cancel()
			if err == nil && (want == "" || semver.Canonical(v) == want) {
				return true
			}
		}
		time.Sleep(time.Second)
	}
	return false
}

// ResetGowa switches back to the GoWA version bundled with the app.
func (a *App) ResetGowa() error {
	if a.gu.status.State == GUDownloading || a.gu.status.State == GUInstalling {
		return errGowaBusy
	}
	_ = a.Store.SetSetting(context.Background(), keyGowaOverride, "{}")
	a.Super.SetExe(a.gu.bundled)
	a.Super.Restart()
	go func() {
		a.waitGowaVersion(context.Background(), "")
		a.refreshGowaVersion(context.Background())
	}()
	return nil
}

func (a *App) registerGowaAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/gowa", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.GowaStatus()) })
	mux.HandleFunc("POST /api/gowa/check", func(w http.ResponseWriter, r *http.Request) {
		st, err := a.CheckGowaUpdate(r.Context())
		if err != nil {
			writeJSON(w, 200, map[string]any{"status": st, "error": "errors.gowa_check_failed", "detail": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"status": st})
	})
	mux.HandleFunc("POST /api/gowa/install", func(w http.ResponseWriter, r *http.Request) {
		if err := a.InstallGowaUpdate(); err != nil {
			a.fail(w, 400, err)
			return
		}
		writeJSON(w, 200, a.GowaStatus())
	})
	mux.HandleFunc("POST /api/gowa/reset", func(w http.ResponseWriter, r *http.Request) {
		if err := a.ResetGowa(); err != nil {
			a.fail(w, 400, err)
			return
		}
		writeJSON(w, 200, a.GowaStatus())
	})
}
