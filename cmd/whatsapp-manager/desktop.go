//go:build windows

package main

import (
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"

	"fyne.io/systray"
	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"whatsapp-manager/assets"
	"whatsapp-manager/internal/app"
	"whatsapp-manager/internal/gowa"
	"whatsapp-manager/internal/winx"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// desktop owns the tray icon and the app window.
type desktop struct {
	app     *app.App
	dataDir string

	mu      sync.Mutex
	view    webview2.WebView
	opening bool
	ready   bool
	state   app.TrayState

	mOpen, mStatus, mPause, mLang, mAr, mEn, mQuit *systray.MenuItem
}

func newDesktop(dataDir string) *desktop {
	return &desktop{dataDir: dataDir, state: app.TrayState{Lang: "ar", Phase: gowa.PhaseStarting, WA: gowa.WAUnknown}}
}

// run starts the tray loop (blocks until Quit).
func (d *desktop) run(openWindow bool) {
	systray.Run(func() { d.onReady(openWindow) }, func() {
		d.mu.Lock()
		v := d.view
		d.mu.Unlock()
		if v != nil {
			v.Dispatch(func() { v.Destroy() })
		}
	})
}

func (d *desktop) onReady(openWindow bool) {
	systray.SetIcon(assets.TrayGray)
	systray.SetTitle("WhatsApp Manager")
	systray.SetOnTapped(d.showWindow)
	d.mOpen = systray.AddMenuItem("", "")
	d.mStatus = systray.AddMenuItem("", "")
	d.mStatus.Disable()
	systray.AddSeparator()
	d.mPause = systray.AddMenuItemCheckbox("", "", false)
	d.mLang = systray.AddMenuItem("", "")
	d.mAr = d.mLang.AddSubMenuItemCheckbox("العربية", "", true)
	d.mEn = d.mLang.AddSubMenuItemCheckbox("English", "", false)
	systray.AddSeparator()
	d.mQuit = systray.AddMenuItem("", "")

	d.mu.Lock()
	d.ready = true
	st := d.state
	d.mu.Unlock()
	d.applyTray(st)

	go func() {
		for {
			select {
			case <-d.mOpen.ClickedCh:
				d.showWindow()
			case <-d.mPause.ClickedCh:
				if d.app != nil {
					d.app.SetPaused(!d.mPause.Checked())
				}
			case <-d.mAr.ClickedCh:
				if d.app != nil {
					d.app.SetLanguage("ar")
				}
			case <-d.mEn.ClickedCh:
				if d.app != nil {
					d.app.SetLanguage("en")
				}
			case <-d.mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
	if openWindow {
		d.showWindow()
	}
}

var trayText = map[string]map[string]string{
	"ar": {
		"open": "فتح WhatsApp Manager", "pause": "إيقاف جميع القواعد مؤقتاً", "lang": "اللغة", "quit": "خروج",
		"connected": "واتساب: متصل", "disconnected": "واتساب: غير متصل", "logged_out": "واتساب: غير مربوط",
		"starting": "جارٍ التشغيل…", "error": "خطأ في خدمة واتساب", "paused": " (متوقف مؤقتاً)",
	},
	"en": {
		"open": "Open WhatsApp Manager", "pause": "Pause all rules", "lang": "Language", "quit": "Quit",
		"connected": "WhatsApp: connected", "disconnected": "WhatsApp: disconnected", "logged_out": "WhatsApp: not linked",
		"starting": "Starting…", "error": "WhatsApp service error", "paused": " (paused)",
	},
}

func (d *desktop) setTrayState(st app.TrayState) {
	d.mu.Lock()
	d.state = st
	ready := d.ready
	d.mu.Unlock()
	if ready {
		d.applyTray(st)
	}
}

func (d *desktop) applyTray(st app.TrayState) {
	t := trayText[st.Lang]
	if t == nil {
		t = trayText["ar"]
	}
	status, icon := t["starting"], assets.TrayGray
	switch {
	case st.Phase == gowa.PhaseError:
		status, icon = t["error"], assets.TrayRed
	case st.WA == gowa.WAConnected:
		status, icon = t["connected"], assets.TrayGreen
	case st.WA == gowa.WADisconnected:
		status, icon = t["disconnected"], assets.TrayYellow
	case st.WA == gowa.WALoggedOut:
		status, icon = t["logged_out"], assets.TrayRed
	}
	if st.Paused {
		status += t["paused"]
		if st.WA == gowa.WAConnected {
			icon = assets.TrayYellow
		}
	}
	systray.SetIcon(icon)
	systray.SetTooltip("WhatsApp Manager — " + status)
	d.mOpen.SetTitle(t["open"])
	d.mStatus.SetTitle(status)
	d.mPause.SetTitle(t["pause"])
	if st.Paused {
		d.mPause.Check()
	} else {
		d.mPause.Uncheck()
	}
	d.mLang.SetTitle(t["lang"])
	if st.Lang == "en" {
		d.mEn.Check()
		d.mAr.Uncheck()
	} else {
		d.mAr.Check()
		d.mEn.Uncheck()
	}
	d.mQuit.SetTitle(t["quit"])
}

// showWindow opens the window, or brings the existing one to the front.
func (d *desktop) showWindow() {
	if d.app == nil {
		return
	}
	d.mu.Lock()
	if v := d.view; v != nil {
		d.mu.Unlock()
		v.Dispatch(func() {
			h := uintptr(v.Window())
			procShowWindow.Call(h, 9) // SW_RESTORE
			procSetForegroundWindow.Call(h)
		})
		return
	}
	if d.opening {
		d.mu.Unlock()
		return
	}
	d.opening = true
	d.mu.Unlock()

	go func() {
		// A WebView2 window must live on one OS thread with its own message loop.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		url := d.app.UIURL()
		w := webview2.NewWithOptions(webview2.WebViewOptions{
			Debug:     false,
			AutoFocus: true,
			DataPath:  filepath.Join(d.dataDir, "webview2"),
			WindowOptions: webview2.WindowOptions{
				Title:  "WhatsApp Manager",
				Width:  1180,
				Height: 800,
				IconId: 1,
				Center: true,
			},
		})
		if w == nil {
			// WebView2 runtime missing: use an Edge app window or the browser.
			slog.Warn("webview2 unavailable; falling back to browser")
			d.mu.Lock()
			d.opening = false
			d.mu.Unlock()
			if err := winx.OpenEdgeApp(url); err != nil {
				_ = winx.OpenURL(url)
			}
			return
		}
		w.SetSize(880, 600, webview2.HintMin)
		w.Navigate(url)
		d.mu.Lock()
		d.view = w
		d.opening = false
		d.mu.Unlock()
		w.Run() // returns when the window is closed
		d.mu.Lock()
		d.view = nil
		d.mu.Unlock()
		d.app.CloseHint()
	}()
}
