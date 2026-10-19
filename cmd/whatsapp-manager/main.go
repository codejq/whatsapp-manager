//go:build windows

// Command whatsapp-manager is the desktop app: a tray icon, a WebView2
// window and the background services that forward WhatsApp messages.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"whatsapp-manager/internal/app"
	"whatsapp-manager/internal/winx"
)

// Set with -ldflags "-X main.version=... -X main.updateRepo=owner/repo -X main.channel=github|store".
var (
	version    = "dev"
	updateRepo = ""
	channel    = "github"
)

func main() {
	minimized := flag.Bool("minimized", false, "start in the tray without opening the window")
	dataDirFlag := flag.String("data-dir", "", "override the data directory (testing)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	app.Version, app.UpdateRepo, app.Channel = version, updateRepo, channel
	// The MSIX StartupTask cannot pass --minimized; a packaged launch right
	// after Windows starts is treated as an autostart.
	if winx.IsPackaged() && winx.Uptime() < 5*time.Minute {
		*minimized = true
	}

	dataDir := *dataDirFlag
	if dataDir == "" {
		d, err := winx.DataDir()
		if err != nil {
			fatal("Cannot create data folder", err)
		}
		dataDir = d
	} else if err := os.MkdirAll(dataDir, 0o700); err != nil {
		fatal("Cannot create data folder", err)
	}

	defer func() {
		if r := recover(); r != nil {
			crash := filepath.Join(dataDir, "logs", "crash-"+time.Now().Format("20060102-150405")+".txt")
			_ = os.MkdirAll(filepath.Dir(crash), 0o700)
			_ = os.WriteFile(crash, []byte(fmt.Sprintf("%v\n\n%s", r, debug.Stack())), 0o600)
			winx.MessageBox("WhatsApp Manager", fmt.Sprintf("Unexpected error. Details were saved to:\n%s", crash))
			os.Exit(2)
		}
	}()

	first, err := winx.AcquireSingleInstance()
	if err != nil {
		fatal("Startup failed", err)
	}
	if !first {
		// Another instance runs: ask it to show its window and exit.
		for i := 0; i < 10; i++ {
			if app.SignalRunningInstance(dataDir) == nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		return
	}

	ui := newDesktop(dataDir)
	a, err := app.New(app.Config{
		DataDir:   dataDir,
		GowaExe:   findGowa(),
		Platform:  &platform{},
		OnTray:    ui.setTrayState,
		OnShow:    ui.showWindow,
		Minimized: *minimized,
		// A custom data dir means a test/dev run: never touch autostart.
		NoAutostart: *dataDirFlag != "",
	})
	if err != nil {
		fatal("Startup failed", err)
	}
	ui.app = a
	if err := a.Start(); err != nil {
		fatal("Startup failed", err)
	}
	ui.run(!*minimized) // blocks until Quit
	a.Stop()
}

// findGowa locates the bundled GoWA binary.
func findGowa() string {
	if p := os.Getenv("WAM_GOWA_EXE"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(exe), "gowa", "whatsapp.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Development: binary fetched by build/fetch-gowa.ps1.
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, "build", "gowa", "whatsapp.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if exe != "" {
		return filepath.Join(filepath.Dir(exe), "gowa", "whatsapp.exe")
	}
	return "whatsapp.exe"
}

func fatal(msg string, err error) {
	winx.MessageBox("WhatsApp Manager", fmt.Sprintf("%s:\n%v", msg, err))
	os.Exit(1)
}
