# Starter stack: Windows desktop apps for non-technical users

A **self-contained blueprint** for new projects. It describes the stack, the architecture, the icon
generator, the build and release pipeline and the lessons learned. The **appendices contain every
reusable file in full**, so you can start a project from this document alone; no other repository is
needed.

Use it for any Windows desktop app that must:

- install with one click and need no setup;
- run in the background (system tray) and start with Windows;
- have a modern web-style interface, optionally in Arabic (RTL) and English;
- ship on GitHub Releases **and** the Microsoft Store from one codebase.

It was taken from a production app built in October 2026; the versions below are from then. Take the
newest stable versions when you start.

**Placeholders used throughout:**

| Placeholder | Replace with |
|---|---|
| `MyApp` | Code name without spaces (exe name, data folder, mutex, registry value) |
| `My App` | Display name |
| `myapp` | Go module path and lowercase identifiers |
| `OWNER/REPO` | GitHub repository |
| `Your Company` | Publisher name |
| `{{NEW-GUID}` | A new GUID for the installer (`[guid]::NewGuid()` in PowerShell; keep the leading `{{`) |
| `MYAPP_` | Prefix for environment variables |

---

## 1. The stack at a glance

| Layer | Choice | Why |
|---|---|---|
| App core | **Go** (1.27), `CGO_ENABLED=0` | One static `.exe`, no runtime to install, easy Windows API access, fast builds |
| Window | **WebView2** via `github.com/jchv/go-webview2` | Built into Windows 10/11, no CGO; falls back to an Edge `--app` window, then the browser |
| Tray icon | `fyne.io/systray` | Pure Go, left-click and right-click menu, swappable icons |
| UI | **Vue 3** + **Vite** + **Tailwind CSS 4** + `vue-i18n` 11 + `vue-router` (hash mode) | Fast to build, small output; Tailwind's logical utilities make RTL free |
| Fonts | `@fontsource/ibm-plex-sans-arabic` + `@fontsource/inter` (bundled) | Works offline; good Arabic and Latin pairing |
| UI ↔ core | Local HTTP API on `127.0.0.1:<random port>` + embedded SPA (`go:embed`) | One process serves both; the window just loads a URL |
| Storage | **SQLite** via `modernc.org/sqlite` (pure Go), WAL, embedded SQL migrations | No CGO, one file, transactional |
| Secrets | **Windows DPAPI** (`golang.org/x/sys/windows`) | Encrypted per Windows user, no key management |
| Email (if needed) | `github.com/wneessen/go-mail` | SMTP with STARTTLS/SSL/auth types; easy error classification |
| Logs | `log/slog` + `gopkg.in/natefinch/lumberjack.v2` | Rotating files in the data folder |
| Text | `golang.org/x/text` (NFKC, case folding) | Arabic-aware matching |
| Images | `golang.org/x/image/draw` | The icon generator (section 6) |
| Exe resources | `go-winres` (`build/winres/winres.json`) | Icon, version info, DPI-aware manifest |
| Installer | **Inno Setup 6** (per-user, Arabic + English) | One `Setup.exe`, no admin, uninstaller |
| Store package | **MSIX** (`makeappx` from the Windows SDK) | Microsoft signs it; no certificate needed |
| CI/CD | GitHub Actions (`windows-latest`) | Build, test, release, Store submission, website |
| Website | Static `docs/index.html` + Jekyll for Markdown, on GitHub Pages | Free hosting, privacy-policy URL for the Store |
| Signing | SignPath Foundation (free for open source) for GitHub builds; Microsoft for the Store | No paid certificate needed |

**Tools to install** (Windows, PowerShell as the user):

```powershell
winget install GoLang.Go OpenJS.NodeJS.LTS JRSoftware.InnoSetup GitHub.cli
winget install Microsoft.WindowsSDK.10.0.26100          # signtool, makeappx, WACK (MSIX only)
go install github.com/tc-hib/go-winres@latest           # exe icon/version resources
go install github.com/rhysd/actionlint/cmd/actionlint@latest   # workflow linting
```

**Go dependencies:**

```powershell
go get fyne.io/systray github.com/jchv/go-webview2 golang.org/x/sys golang.org/x/text golang.org/x/image `
       modernc.org/sqlite gopkg.in/natefinch/lumberjack.v2 github.com/wneessen/go-mail
```

**npm dependencies** (in `web/`):

```powershell
npm i vue vue-router vue-i18n @fontsource/ibm-plex-sans-arabic @fontsource/inter
npm i -D vite @vitejs/plugin-vue tailwindcss @tailwindcss/vite eslint eslint-plugin-vue @eslint/js globals
```

---

## 2. Architecture pattern

```
MyApp.exe (Go, one process)
 ├─ main: single-instance lock → load settings → start services → tray loop (blocks)
 ├─ tray (fyne systray): status icon, Open / Pause / Quit
 ├─ window (WebView2, own OS thread): loads http://127.0.0.1:<port>/#/?t=<token>
 ├─ local HTTP server (127.0.0.1 only)
 │    ├─ /            embedded SPA (go:embed of web/dist)
 │    ├─ /api/*       JSON API, requires the per-launch token + Host/Origin check
 │    ├─ /hook        signed webhooks from a helper process (if any)
 │    └─ /ipc/show    "second instance asks first instance to show its window"
 ├─ background workers (each in goSafe(): recover + restart on panic)
 └─ helper supervisor (optional): runs a bundled .exe as a child process
      job object KILL_ON_JOB_CLOSE, health checks, restart with backoff,
      fallback to the bundled version if an updated helper fails to start
```

The code for all of this is in **Appendix B**.

Rules that keep it robust:

- **Everything listens on loopback only.** The UI API needs a random per-launch token, delivered once in the
  URL fragment and kept in `sessionStorage`. Make the token **hex**, because the SPA's regex expects it.
  The server checks `Host` and `Origin` to stop DNS rebinding and CSRF.
- **All writable data lives in `%LOCALAPPDATA%\MyApp\`**, never next to the exe. MSIX install folders are
  read-only, and Windows redirects AppData for packaged apps.
- **Single instance:** a named mutex `Local\MyApp-SingleInstance`. The second launch posts to `/ipc/show`
  (port and token in `instance.json`) and exits. Dev runs with `--data-dir` add a hash of the folder to the
  mutex name, so they can run beside an installed copy, and they never register autostart.
- **Child processes** go in a Job Object with `KILL_ON_JOB_CLOSE`, and use `CREATE_NO_WINDOW`.
- **WebView2 on its own OS thread** (`runtime.LockOSThread`). Closing the window destroys it (freeing
  memory) while the app keeps running in the tray. Set `DataPath` to the data folder.
- **Autostart:** unpackaged builds use `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` with
  `--minimized`. MSIX builds use a `windows.startupTask`, which can't pass arguments, so a packaged launch
  within 5 minutes of boot is treated as an autostart.
- **Packaged detection:** `GetCurrentPackageFullName`. Use it to turn off self-update and registry
  autostart in the Store build.
- **Crash safety:** a top-level `recover` writes `logs\crash-*.txt` and shows a message box. The database
  runs `PRAGMA integrity_check` at start and restores the newest daily backup if it is damaged.

### Reliable background work (inbox → outbox)

For any "react to events and deliver somewhere" app (schema in **Appendix B.8**):

1. The receiver only **stores** each event, with a unique key (`UNIQUE(source, id, event)`, using
   `INSERT OR IGNORE`), and returns quickly. Retries become no-ops. If a source can send an empty
   placeholder event before the real one with the same ID, ignore content-less events before inserting.
2. A worker turns inbox rows into **outbox jobs in one transaction**, with
   `UNIQUE(source, id, destination)`, so a destination never gets the same item twice.
3. Dispatchers **claim** jobs (`pending → sending`), send, then mark `sent` or `failed`, with backoff
   retries. Failures that aren't the job's fault (offline) don't use up retries.
4. At startup, jobs still in `sending` become **`uncertain`** and are never resent automatically
   (at-most-once). The UI offers "Send again".
5. Record the IDs of what you sent, so echoes are ignored (loop guard).
6. Clean up history after N days (a setting), but never delete pending jobs.

---

## 3. Repository layout

```
cmd/myapp/            main.go, desktop.go (tray + window), platform.go          → Appendix B.1–B.3
internal/app/         server.go (+ your settings, API, workers)                  → Appendix B.4
internal/winx/        winx.go, toast.go (Windows APIs)                           → Appendix B.5–B.6
internal/store/       store.go + migrations/*.sql (embedded)                     → Appendix B.7–B.8
internal/ui/          ui.go (//go:embed all:dist) + dist/.gitkeep               → Appendix B.9
internal/<domain>/    your pure logic, with table-driven tests
assets/               assets.go (tray icons) + generated icons/ msix/ store/    → Appendix B.10, C
tools/genicons/       icon generator                                             → Appendix C
web/                  Vue app; web/go.mod (see gotchas)                          → Appendix D
build/                fetch-helper.ps1, *.version, winres/, inno/, msix/         → Appendix E, F
.github/workflows/    ci, release, store, pages; dependabot.yml                  → Appendix G
build.ps1             one entry point for local and CI builds                    → Appendix E.1
docs/                 PLAN.md, RELEASING.md, privacy.md, user guides, index.html, screenshots/
PROJECT_CONTEXT.md    durable facts and invariants for any agent; CLAUDE.md → "@PROJECT_CONTEXT.md" → Appendix H
.gitattributes, .gitignore                                                       → Appendix A
LICENSE, THIRD_PARTY_NOTICES.md, README.md (+ README.ar.md), CONTRIBUTING.md, SECURITY.md
```

---

## 4. UI conventions (Vue + Tailwind 4 + i18n)

Setup files are in **Appendix D**.

- **Arabic first (if your audience is Arabic):** `<html dir>` and `lang` are set from the language setting,
  and every string is in both `ar.json` and `en.json`. Check that the two key sets match before release.
- **Use logical CSS only:** `ms-`, `me-`, `ps-`, `pe-`, `start-`, `end-`, `text-start`, and `rtl:` variants
  for icons that point a direction. Never `ml-`, `mr-`, `left-` or `right-`.
- **Tailwind 4:** custom component classes must be declared with `@utility name { @apply …; }` to be
  reusable with `@apply`; a plain `.class` inside `@layer components` won't work. Dark mode uses
  `@custom-variant dark (&:where(.dark, .dark *));`. Brand colours go in `@theme { --color-brand-500: …; }`.
- **vue-i18n:** a bare `@` in a message is parsed as a linked message, so write `{'@'}`. Use
  `legacy: false`. The default JIT compiler needs no `unsafe-eval`, so the strict CSP keeps working.
- **Errors:** the backend returns `{error: "errors.some.key", detail}`; the UI translates the key, with a
  generic fallback.
- **Numbers and phone numbers:** wrap them in a `.ltr { direction: ltr; unicode-bidi: isolate }` span
  inside RTL text.
- **Lists:** join with the locale's comma (`'، '` in Arabic, `', '` in English).
- **State:** a reactive `app` store refreshed by polling `/api/state` every 2–3 s is plenty for a local app.
- **Plain-language features:** show each rule or setting as a sentence ("When … and … then …"), offer
  wizards with templates and a "Try it" preview, and have the activity log state the reason for every
  decision.

---

## 5. Build, packaging, release

`build.ps1 [-Version x.y.z] [-Installer] [-Msix] [-Channel github|store] [-SkipUI] [-SkipTests]`
(Appendix E.1):

1. `npm ci && npm run build` writes to `internal/ui/dist`. A Vite plugin re-creates `.gitkeep`.
2. Fetch bundled helpers (if any) from pinned `build/*.version` files, verifying SHA-256
   (Appendix E.2).
3. Run `go vet ./...` and `go test ./...`.
4. `go-winres make --product-version --file-version`, which creates `rsrc_windows_amd64.syso` (gitignored).
5. `go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=… -X main.channel=… -X main.updateRepo=OWNER/REPO"`.
6. Stage `dist/app/` (exe, helpers, LICENSE, notices) and make the portable `.zip`.
7. Build the Inno `Setup.exe` (Appendix F.1):
   - `PrivilegesRequired=lowest`, `{userpf}` install directory, Arabic and English.
   - `AppUserModelID` on the shortcut, so toasts work.
   - Kill the running app before installing and uninstalling.
   - Remove the `Run` value on uninstall, and ask whether to delete the data folder.
8. Build the MSIX (Appendix F.2–F.4):
   - The `AppxManifest.xml` template has `runFullTrust` and a `startupTask`.
   - The identity comes from `build/msix/store.json` (committed; not secret), which `store.local.json`
     and `MSIX_*` environment variables can override.
   - Store versions end in `.0`.
9. Write `checksums.txt`.

**Workflows** (Appendix G; check them with `actionlint`):

| File | Trigger | Notes |
|---|---|---|
| `ci.yml` | push, PR, manual | lint, test, full build; the artifact is uploaded **only on manual runs** (saves Actions storage) |
| `release.yml` | tag `vX.Y.Z` | builds, optional SignPath signing, `gh release create` (build the argument list as an array; a `-` in the tag means pre-release) |
| `store.yml` | after a stable release, or manual | builds the MSIX, `msstore reconfigure` + `msstore publish <msix> -id <StoreID>`; gated by the variable `STORE_ENABLED` |
| `pages.yml` | changes under `docs/` | checks Pages is enabled first (skips with a notice), then Jekyll build and deploy |

**In-app updates:** the GitHub build checks the GitHub Releases API (`updateRepo` from ldflags) and only
shows a banner. Bundled helpers can update themselves within the same major version, with checksum
verification and automatic rollback. All of this is disabled in the Store build, because Store policy
forbids downloading executables.

---

## 6. App icon: generated from code

The app has no hand-drawn icon files. `go run ./tools/genicons` (full source in **Appendix C**) draws
everything from one Go function, so icons are reproducible, easy to restyle, and the tray status variants
always match.

**How it draws:**
- The logo is drawn at **4× supersampling**, then downscaled with `draw.CatmullRom` for smooth edges at
  16 px.
- The shape is a rounded square with a diagonal brand gradient (`#2BB673 → #0F7A45`), a white speech
  bubble with a tail, and a forward arrow in brand green.
- A status variant adds a dot at the bottom right: a white ring around a coloured centre.
- `.ico` files are written with **PNG-compressed entries** (fine on Vista and later), so one `.ico` holds
  every size.

**Outputs:**

| File | Sizes | Used by |
|---|---|---|
| `assets/icons/app.ico` | 16, 20, 24, 32, 40, 48, 64, 128, 256 | exe resource (`go-winres`), installer, WebView2 window (`IconId: 1`) |
| `assets/icons/app-256.png`, `app-512.png` | 256, 512 | README, favicon, UI logo |
| `assets/icons/tray-{green,yellow,red,gray}.ico` | 16, 20, 24, 32, 48 | tray: ok, warning or paused, needs action, starting |
| `assets/msix/Square44x44Logo*.png` | 44 + targetsize 24, 48, 256 unplated | MSIX taskbar and Start icons |
| `assets/msix/Square150x150Logo.png`, `Wide310x150Logo.png`, `SplashScreen.png` (620×300) | tiles | MSIX |
| `assets/msix/StoreLogo.png` | 50 | MSIX |
| `assets/store/app-icon-300.png`, `box-art-1080.png`, `poster-720x1080.png` | 300, 1080, 720×1080 | Partner Center listing |

**To restyle:** change `green1`/`green2`, the bubble geometry in `render`, and the glyph in `inArrow`
(any shape made of rectangles, triangles and circles works). Run the generator and commit the outputs.
Tray icons are embedded with `//go:embed` (Appendix B.10).

**Avoid third-party trademarks** in the icon and the Store name. A generic chat bubble with an arrow passed
Store certification; another company's brand name in your app's name is a risk.

---

## 7. Testing approach

- **Pure logic:** table-driven Go tests (matching rules, normalization, link detection, semver).
- **Storage:** tests on a temporary database: idempotent inserts, duplicate destinations, claim, crash
  recovery and purge.
- **End-to-end in-process:** `httptest` webhook → engine → dispatcher with fake senders. Assert exactly-once
  delivery and the loop guards.
- **External services:** a fake SMTP server (raw socket) and a fake helper REST server for screenshots and
  demos.
- **Live checks on the real app:**
  - Run with `--data-dir` (isolated; never touches autostart) and `MYAPP_UI_TOKEN=<hex>` to call the API
    from scripts.
  - Decrypt DPAPI secrets in PowerShell with `[Security.Cryptography.ProtectedData]::Unprotect(bytes,
    entropy, 'CurrentUser')`, using the same entropy as `winx`, to sign test webhooks.
- **UI screenshots:** headless Edge (`msedge --headless=new --force-device-scale-factor=1.5
  --window-size=1280,800 --virtual-time-budget=9000 --screenshot=out.png URL`), with a separate
  `--user-data-dir` per shot. Use a **demo instance with fictional data**; never screenshot real user data.
- **Installer:** silent install and uninstall (`/VERYSILENT /SUPPRESSMSGBOXES /NORESTART`), then check files,
  shortcut and uninstall entry (`HKCU\...\Uninstall\{GUID}_is1`).
- **MSIX:**
  1. Test-sign with a temporary self-signed certificate (subject equal to the manifest Publisher), trusted in
     `LocalMachine\TrustedPeople`, then `Add-AppxPackage`.
  2. Launch through the app execution alias with `--data-dir`.
  3. Run WACK (`appcert.exe test -packagefullname … -testid "[31,45,46,49,52,53,61,62,63,66,70,75,77,83,86,87,88,89,92]"`).
  4. Remove the package and the certificate.

---

## 8. Publishing checklist (what actually worked)

1. **GitHub:** create an empty repo, push, and turn on Pages with Source = **GitHub Actions**. Workflow
   files can only be pushed with a credential that has the `workflow` scope.
2. **First release:** `git tag v0.1.0 && git push origin v0.1.0`. GitHub Release files don't count
   against Actions storage.
3. **SignPath Foundation:** apply at signpath.org after the first public release (open source only), then
   add the `SIGNPATH_*` variables and secret. Until then, tell users about SmartScreen's
   "More info → Run anyway".
4. **Microsoft Store:**
   - Register at storedeveloper.microsoft.com. Individual accounts are free; a company account shows a
     company name.
   - Create the product as **"MSIX or PWA app"**, not "EXE or MSI". The EXE/MSI type requires your own
     trusted code signature and a download URL.
   - Copy Product identity into `build/msix/store.json`.
   - Import the listing as CSV: export the template, fill it, and import the **folder** with the images.
   - Age rating: IARC questionnaire.
   - Explain why the package needs `runFullTrust`: a desktop app that runs a bundled child process, a tray
     icon and a WebView2 UI, all locally.
   - The first submission is by hand. Then create an Entra app in ⚙ Account settings → User management →
     Microsoft Entra applications (Manager role), and add `STORE_TENANT_ID`, `STORE_SELLER_ID`,
     `STORE_CLIENT_ID` and `STORE_CLIENT_SECRET` to a GitHub environment named `store`. Set the repository
     variable `STORE_ENABLED=true`.
5. **Privacy policy URL:** `https://OWNER.github.io/REPO/privacy.html` (from `docs/privacy.md`).

---

## 9. Gotchas we hit, and the fixes

| Symptom | Cause | Fix |
|---|---|---|
| `go ./...` builds a Go file inside `node_modules` | an npm package ships Go code | add `web/go.mod` (`module myapp/web`) to cut `web/` out of the main module |
| gofmt fails in CI on Windows | CRLF line endings | `.gitattributes`: `* text=auto eol=lf` (with `*.ps1`/`*.iss` as CRLF) |
| UI says "session expired" in tests | test token wasn't hex | always use hex tokens |
| `@apply btn` fails | Tailwind 4 | declare with `@utility` |
| i18n message breaks on `name@example.com` | linked-message syntax | `{'@'}` |
| A dev copy can't start while the installed app runs | shared single-instance mutex | scope the mutex by data folder |
| A dev run registered itself for autostart | first-run autostart | skip autostart when `--data-dir` is given |
| Two webhook events for one message, the real one dropped | the source sends an empty "carrier" event first with the same ID | ignore events without content before inserting |
| Links glued to text not detected | `\b` before `https://` | match scheme links without a boundary; generic TLDs for bare domains, excluding file extensions |
| Email clients merge forwarded emails | identical subjects | optional running number in the subject (`[App #42]`) |
| Pages workflow fails "Not Found" | Pages not enabled | turn it on in Settings; the workflow checks first and skips with a notice |
| Release step failed on stable tags | empty `--prerelease` argument passed to `gh` | build the argument array conditionally |
| GitHub rejects the push of workflow files | the OAuth token lacks `workflow` scope | push with a credential that has it (`gh auth refresh -s workflow`) |
| Store product had no "Product identity" | created as EXE/MSI | create a new **MSIX** product |
| Actions storage quota hit | artifacts on every CI run | upload only on manual runs; short retention |
| Headless Edge screenshots blank or missing | shared profile between runs | a separate `--user-data-dir` for each screenshot |
| Phone-width screenshots look clipped | headless Edge's minimum window is about 500 px | judge mobile layout from CSS (`minmax(0,1fr)`), not those shots |
| Bash heredoc fails with Arabic text | shell quoting of non-ASCII content | write such files with an editor or a script, not a heredoc |

---

## 10. New-project checklist

- [ ] `go mod init myapp`; create the files from Appendices A–H; replace the placeholders (table at the top)
- [ ] New installer GUID in `setup.iss`; `AppName` in `winx`; `AppUserModelID`; window and tray titles
- [ ] Restyle the icon in `tools/genicons`, then `go run ./tools/genicons`; copy `assets/icons/app-256.png` to
      `web/src/logo.png` and `web/public/favicon.png`
- [ ] Add `LICENSE`; create `internal/ui/dist/.gitkeep`; `go mod tidy`
- [ ] `npm --prefix web ci && npm --prefix web run build`; `./build.ps1` (compiles a working tray app)
- [ ] Write `PROJECT_CONTEXT.md` (decisions + invariants) and `CLAUDE.md` (`@PROJECT_CONTEXT.md`)
- [ ] `ar.json` / `en.json` with the same keys; pick the default language for your audience
- [ ] Your settings (DPAPI for secrets), autostart toggle, SQLite migrations, daily backup, cleanup setting
- [ ] Tests: pure logic, storage, end-to-end with fakes
- [ ] `docs/privacy.md`, user guides, `docs/index.html` website, screenshots from a demo instance
- [ ] CI green → tag `v0.1.0` → apply for SignPath → Store (MSIX) → automate Store submissions

---

# Appendices: complete files

Every file below is complete and ready to use. The Go files in Appendix B were compiled together as a
skeleton (`go vet` clean) before this document was written. Paths are relative to the repository root.

## Appendix A: Repository basics

### A.1 `.gitattributes`

```gitattributes
* text=auto eol=lf
*.ps1 text eol=crlf
*.iss text eol=crlf
*.bat text eol=crlf
*.png binary
*.ico binary
*.syso binary
```

### A.2 `.gitignore`

```gitignore
# Build output
/dist/
/bin/
*.exe
/build/bin/
/internal/ui/dist/*
!/internal/ui/dist/.gitkeep
/cmd/myapp/rsrc_windows_*.syso

# Runtime data
*.db
*.db-wal
*.db-shm
logs/

# Frontend
/web/node_modules/
/web/dist/

# Local secrets / identity overrides
.env
*.pfx
/build/msix/store.local.json

# OS / editor
.DS_Store
Thumbs.db
.vscode/
.idea/
```

## Appendix B: Go core (compiles as a skeleton)

### B.1 `cmd/myapp/main.go`

```go
//go:build windows

// Command myapp is the desktop app: tray icon, WebView2 window and background services.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"myapp/internal/app"
	"myapp/internal/winx"
)

// Set with -ldflags "-X main.version=... -X main.channel=github|store -X main.updateRepo=owner/repo".
var (
	version    = "dev"
	updateRepo = ""
	channel    = "github"
)

func main() {
	minimized := flag.Bool("minimized", false, "start in the tray without opening the window")
	dataDirFlag := flag.String("data-dir", "", "override the data folder (testing; never touches autostart)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	app.Version, app.UpdateRepo, app.Channel = version, updateRepo, channel
	// The MSIX StartupTask cannot pass --minimized: treat a packaged launch
	// shortly after boot as an autostart.
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
			winx.MessageBox("My App", "Unexpected error. Details were saved to:\n"+crash)
			os.Exit(2)
		}
	}()

	// One instance per data folder; a second launch asks the first to show its window.
	scope := ""
	if *dataDirFlag != "" {
		h := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(dataDir))))
		scope = hex.EncodeToString(h[:8])
	}
	first, err := winx.AcquireSingleInstance(scope)
	if err != nil {
		fatal("Startup failed", err)
	}
	if !first {
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
		DataDir:     dataDir,
		Platform:    &platform{},
		OnTray:      ui.setTrayState,
		OnShow:      ui.showWindow,
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

func fatal(msg string, err error) {
	winx.MessageBox("My App", fmt.Sprintf("%s:\n%v", msg, err))
	os.Exit(1)
}
```

### B.2 `cmd/myapp/desktop.go`: tray + WebView2 window

```go
//go:build windows

package main

import (
	"path/filepath"
	"runtime"
	"sync"

	"fyne.io/systray"
	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"myapp/assets"
	"myapp/internal/app"
	"myapp/internal/winx"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// desktop owns the tray icon and the (optional) app window.
type desktop struct {
	app     *app.App
	dataDir string

	mu      sync.Mutex
	view    webview2.WebView
	opening bool
	ready   bool
	state   app.TrayState

	mOpen, mStatus, mPause, mQuit *systray.MenuItem
}

func newDesktop(dataDir string) *desktop {
	return &desktop{dataDir: dataDir, state: app.TrayState{Lang: "en", Status: "starting"}}
}

// run starts the tray loop on the main goroutine (blocks until Quit).
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
	systray.SetTitle("My App")
	systray.SetOnTapped(d.showWindow) // left click opens the window; right click shows the menu
	d.mOpen = systray.AddMenuItem("", "")
	d.mStatus = systray.AddMenuItem("", "")
	d.mStatus.Disable()
	systray.AddSeparator()
	d.mPause = systray.AddMenuItemCheckbox("", "", false)
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
				d.app.SetPaused(!d.mPause.Checked())
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
	"en": {"open": "Open My App", "pause": "Pause", "quit": "Quit", "ok": "Running", "warn": "Needs attention", "error": "Problem", "starting": "Starting…"},
	"ar": {"open": "فتح التطبيق", "pause": "إيقاف مؤقت", "quit": "خروج", "ok": "يعمل", "warn": "يحتاج انتباهك", "error": "مشكلة", "starting": "جارٍ التشغيل…"},
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
		t = trayText["en"]
	}
	icon := map[string][]byte{"ok": assets.TrayGreen, "warn": assets.TrayYellow, "error": assets.TrayRed}[st.Status]
	if icon == nil {
		icon = assets.TrayGray
	}
	status := t[st.Status]
	if status == "" {
		status = t["starting"]
	}
	systray.SetIcon(icon)
	systray.SetTooltip("My App — " + status)
	d.mOpen.SetTitle(t["open"])
	d.mStatus.SetTitle(status)
	d.mPause.SetTitle(t["pause"])
	if st.Paused {
		d.mPause.Check()
	} else {
		d.mPause.Uncheck()
	}
	d.mQuit.SetTitle(t["quit"])
}

// showWindow opens the WebView2 window or brings the existing one to the front.
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
		// A WebView2 window lives on one OS thread with its own message loop.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		url := d.app.UIURL()
		w := webview2.NewWithOptions(webview2.WebViewOptions{
			AutoFocus: true,
			DataPath:  filepath.Join(d.dataDir, "webview2"), // never next to the exe (MSIX is read-only)
			WindowOptions: webview2.WindowOptions{
				Title: "My App", Width: 1180, Height: 800, IconId: 1, Center: true,
			},
		})
		if w == nil { // WebView2 runtime missing: Edge app window, then the default browser
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
		d.view, d.opening = w, false
		d.mu.Unlock()
		w.Run() // returns when the window is closed; the app keeps running in the tray
		d.mu.Lock()
		d.view = nil
		d.mu.Unlock()
		d.app.CloseHint() // e.g. a one-time toast "still running in the tray"
	}()
}
```

### B.3 `cmd/myapp/platform.go`

```go
//go:build windows

package main

import (
	"sync"
	"syscall"

	"myapp/internal/winx"
)

// platform implements app.Platform with Windows APIs (kept out of internal/app so it stays testable).
type platform struct {
	jobOnce sync.Once
	job     *winx.Job
	jobErr  error
}

func (p *platform) Protect(s string) (string, error)   { return winx.Protect(s) }
func (p *platform) Unprotect(s string) (string, error) { return winx.Unprotect(s) }
func (p *platform) SetAutostart(on bool) error         { return winx.SetAutostart(on) }
func (p *platform) AutostartEnabled() bool {
	if winx.IsPackaged() {
		return true // managed by the MSIX StartupTask (Task Manager → Startup apps)
	}
	return winx.AutostartEnabled()
}
func (p *platform) IsPackaged() bool               { return winx.IsPackaged() }
func (p *platform) Toast(title, msg string)        { winx.Toast(title, msg) }
func (p *platform) OpenFolder(path string) error   { return winx.OpenFolder(path) }
func (p *platform) OpenURL(url string) error       { return winx.OpenURL(url) }
func (p *platform) ProcAttr() *syscall.SysProcAttr { return winx.HideWindowAttr() }

// Assign puts a child process into a kill-on-close job object.
func (p *platform) Assign(pid int) error {
	p.jobOnce.Do(func() { p.job, p.jobErr = winx.NewKillOnCloseJob() })
	if p.jobErr != nil {
		return p.jobErr
	}
	return p.job.Assign(pid)
}
```

### B.4 `internal/app/server.go`: loopback server, token guard, IPC, goSafe

```go
package app

// Core of internal/app: config, the loopback HTTP server with the per-launch
// token guard, second-instance IPC, and panic-safe goroutines. Add your own
// services in Start() and your API routes in registerAPI().

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"myapp/internal/ui"
)

// Build information, set by main via ldflags.
var (
	Version    = "dev"
	UpdateRepo = ""
	Channel    = "github"
)

// Platform is the OS integration (implemented in cmd/myapp/platform.go).
type Platform interface {
	Protect(string) (string, error)
	Unprotect(string) (string, error)
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
	Status string // starting | ok | warn | error
	Paused bool
	Lang   string
}

// Config are startup parameters.
type Config struct {
	DataDir     string
	Platform    Platform
	OnTray      func(TrayState)
	OnShow      func()
	NoAutostart bool
}

// App is the running application.
type App struct {
	cfg      Config
	Log      *slog.Logger
	mu       sync.RWMutex
	token    string
	ipcToken string
	apiPort  int
	server   *http.Server
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	paused   bool
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// New prepares logging and secrets; open your store and load settings here.
func New(cfg Config) (*App, error) {
	a := &App{cfg: cfg}
	logDir := filepath.Join(cfg.DataDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, err
	}
	a.Log = slog.New(slog.NewTextHandler(&lumberjack.Logger{Filename: filepath.Join(logDir, "app.log"), MaxSize: 10, MaxBackups: 5}, nil))
	slog.SetDefault(a.Log)
	a.token = randomHex(24) // hex! the SPA reads it with /[?&]t=([a-f0-9]+)/
	if t := os.Getenv("MYAPP_UI_TOKEN"); len(t) >= 16 {
		a.token = t // automated UI tests only
	}
	a.ipcToken = randomHex(24)
	return a, nil
}

// UIURL is what the window opens (token in the fragment, never logged).
func (a *App) UIURL() string { return fmt.Sprintf("http://127.0.0.1:%d/#/?t=%s", a.apiPort, a.token) }

// Start launches the server and background services.
func (a *App) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	ln, err := net.Listen("tcp", "127.0.0.1:0") // persist the port in settings if other processes need it
	if err != nil {
		return err
	}
	a.apiPort = ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/ipc/show", a.handleIPCShow)
	a.registerAPI(mux)
	a.server = &http.Server{Handler: a.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = a.server.Serve(ln) }()
	a.writeInstanceFile()
	// Start background services here, e.g.: a.goSafe("worker", func() { worker.Run(ctx) })
	_ = ctx
	a.notifyTray("ok")
	return nil
}

// Stop shuts down gracefully.
func (a *App) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
	}
	if a.server != nil {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = a.server.Shutdown(c)
		cancel()
	}
	_ = os.Remove(filepath.Join(a.cfg.DataDir, "instance.json"))
}

// goSafe runs fn and restarts it after a panic.
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

func (a *App) notifyTray(status string) {
	if a.cfg.OnTray != nil {
		a.mu.RLock()
		p := a.paused
		a.mu.RUnlock()
		a.cfg.OnTray(TrayState{Status: status, Paused: p, Lang: "en"})
	}
}

// SetPaused is called from the tray menu.
func (a *App) SetPaused(p bool) {
	a.mu.Lock()
	a.paused = p
	a.mu.Unlock()
	a.notifyTray("ok")
}

// CloseHint runs when the window closes (e.g. a one-time toast).
func (a *App) CloseHint() {}

// guard: loopback Host only (DNS rebinding) and the per-launch token for /api (CSRF).
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host != fmt.Sprintf("127.0.0.1:%d", a.apiPort) && host != fmt.Sprintf("localhost:%d", a.apiPort) {
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
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "errors.unauthorized"})
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"version": Version, "channel": Channel, "packaged": a.cfg.Platform.IsPackaged()})
	})
	// ... your endpoints (Go 1.22+ patterns, e.g. "POST /api/things/{id}") ...

	sub, _ := fs.Sub(ui.FS, "dist")
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			b, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, "UI not built: npm --prefix web run build", 503)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
			_, _ = w.Write(b)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // Vite assets are hashed
		files.ServeHTTP(w, r)
	})
}

// ---- second instance IPC ----

func (a *App) writeInstanceFile() {
	b := fmt.Sprintf(`{"port":%d,"token":%q}`, a.apiPort, a.ipcToken)
	_ = os.WriteFile(filepath.Join(a.cfg.DataDir, "instance.json"), []byte(b), 0o600)
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
	var info struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &info); err != nil {
		return err
	}
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(info.Port)+"/ipc/show", nil)
	req.Header.Set("X-IPC-Token", info.Token)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return errors.New("ipc refused")
	}
	return nil
}
```

### B.5 `internal/winx/winx.go`: DPAPI, autostart, mutex, job object, packaging, shell

```go
//go:build windows

// Package winx wraps the Windows APIs the manager needs: DPAPI secrets,
// autostart, single-instance mutex, job objects, packaging detection and
// opening URLs.
package winx

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// AppName is used for the mutex, registry value and data folder.
const AppName = "MyApp"

// DataDir returns %LOCALAPPDATA%\MyApp (created if missing).
func DataDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, AppName)
	return dir, os.MkdirAll(dir, 0o700)
}

// ---------- DPAPI ----------

var dpapiEntropy = []byte("MyApp/v1")

// Protect encrypts data for the current Windows user and returns base64.
func Protect(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	in := blob([]byte(plain))
	ent := blob(dpapiEntropy)
	var out windows.DataBlob
	if err := windows.CryptProtectData(in, nil, ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

// Unprotect reverses Protect.
func Unprotect(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	in := blob(raw)
	ent := blob(dpapiEntropy)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(in, nil, ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

// ---------- packaging ----------

var (
	modkernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentPackageFullName = modkernel32.NewProc("GetCurrentPackageFullName")
)

// IsPackaged reports whether the process runs from an MSIX package.
func IsPackaged() bool {
	if procGetCurrentPackageFullName.Find() != nil {
		return false
	}
	var n uint32
	r, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&n)), 0)
	const appmodelErrorNoPackage = 15700
	return r != appmodelErrorNoPackage
}

var procGetTickCount64 = modkernel32.NewProc("GetTickCount64")

// Uptime returns the time since Windows started.
func Uptime() time.Duration {
	r, _, _ := procGetTickCount64.Call()
	return time.Duration(r) * time.Millisecond
}

// ---------- autostart ----------

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// AutostartEnabled reports whether the HKCU Run entry points at this exe.
func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(AppName)
	return err == nil && v != ""
}

// SetAutostart adds or removes the HKCU Run entry (unpackaged builds only).
func SetAutostart(enable bool) error {
	if IsPackaged() {
		return errors.New("autostart is managed by Windows for Store installs")
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enable {
		err := k.DeleteValue(AppName)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(AppName, fmt.Sprintf(`"%s" --minimized`, exe))
}

// ---------- single instance ----------

// AcquireSingleInstance creates a per-user named mutex. It returns false if
// another instance already holds it. scope separates instances that use a
// different data folder (dev/test runs); "" is the normal install.
func AcquireSingleInstance(scope string) (bool, error) {
	name := `Local\` + AppName + `-SingleInstance`
	if scope != "" {
		name += "-" + scope
	}
	p, _ := windows.UTF16PtrFromString(name)
	h, err := windows.CreateMutex(nil, false, p)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// The handle is intentionally kept open for the life of the process.
	return true, nil
}

// ---------- job object ----------

// Job kills every assigned process when the manager exits (even on crash).
type Job struct{ h windows.Handle }

// NewKillOnCloseJob creates a job object with KILL_ON_JOB_CLOSE.
func NewKillOnCloseJob() (*Job, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &Job{h: h}, nil
}

// Assign adds a running process to the job.
func (j *Job) Assign(pid int) error {
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(j.h, ph)
}

// HideWindowAttr returns SysProcAttr that launches a console program without a window.
func HideWindowAttr() *syscall.SysProcAttr {
	const createNoWindow = 0x08000000
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// ---------- shell ----------

// OpenURL opens a URL in the default browser.
func OpenURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = HideWindowAttr()
	return cmd.Start()
}

// OpenEdgeApp opens url in a chromeless Microsoft Edge "app" window.
func OpenEdgeApp(url string) error {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return exec.Command(c, "--app="+url).Start()
		}
	}
	return errors.New("edge not found")
}

// OpenFolder opens a folder in Explorer.
func OpenFolder(path string) error {
	return exec.Command("explorer", path).Start()
}

// ---------- messages ----------

var (
	moduser32       = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW = moduser32.NewProc("MessageBoxW")
)

// MessageBox shows a blocking message box (used only for fatal startup errors).
func MessageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	const mbIconError = 0x10
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError)
}

// IsUnder reports whether path is inside dir (after cleaning).
func IsUnder(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
```

### B.6 `internal/winx/toast.go`: Windows toasts

```go
//go:build windows

package winx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AppUserModelID is set on the Start-menu shortcut by the installer so that
// toasts are attributed to the app. Portable installs fall back to PowerShell's ID.
const AppUserModelID = "MyApp.Desktop"

const powershellAUMID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

func toastAppID() string {
	if IsPackaged() {
		return "" // packaged apps cannot be targeted from PowerShell reliably; use fallback ID
	}
	sc := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, "My App.lnk")
	if _, err := os.Stat(sc); err == nil {
		return AppUserModelID
	}
	return ""
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Toast shows a Windows toast notification. It is best-effort and never
// blocks the caller.
func Toast(title, message string) {
	appID := toastAppID()
	if appID == "" {
		appID = powershellAUMID
	}
	xml := `<toast><visual><binding template="ToastGeneric"><text>` + xmlEscape(title) + `</text><text>` + xmlEscape(message) + `</text></binding></visual></toast>`
	script := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null;` +
		`[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null;` +
		`$x = New-Object Windows.Data.Xml.Dom.XmlDocument; $x.LoadXml(` + psQuote(xml) + `);` +
		`$t = New-Object Windows.UI.Notifications.ToastNotification $x;` +
		`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier(` + psQuote(appID) + `).Show($t);`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = HideWindowAttr()
	go func() { _ = cmd.Run() }()
}
```

### B.7 `internal/store/store.go`: SQLite open, embedded migrations, backup

```go
// Package store owns the manager's SQLite database: schema migrations,
// settings, rules, the inbox/outbox queues and maintenance (backup, purge).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the database handle.
type Store struct {
	DB   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and applies migrations.
// Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		dsn = "file:" + filepath.ToSlash(path)
	} else {
		dsn = "file::memory:?cache=shared"
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection avoids SQLITE_BUSY between
	// our own goroutines and keeps transactions strictly serialized.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		v, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %q", name)
		}
		var exists int
		if err := s.DB.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version=?`, v).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?,?)`, v, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// IntegrityCheck runs PRAGMA integrity_check and returns an error if the
// database is damaged.
func (s *Store) IntegrityCheck(ctx context.Context) error {
	var res string
	if err := s.DB.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return errors.New("integrity check failed: " + res)
	}
	return nil
}

// Backup writes a consistent copy of the database into dir and keeps the
// newest `keep` backups.
func (s *Store) Backup(ctx context.Context, dir string, keep int) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Join(dir, "manager-"+time.Now().Format("20060102-150405")+".db")
	if _, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, name); err != nil {
		return "", err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "manager-*.db"))
	sort.Strings(files)
	for len(files) > keep {
		os.Remove(files[0])
		files = files[1:]
	}
	return name, nil
}

// LatestBackup returns the newest backup file in dir, or "".
func LatestBackup(dir string) string {
	files, _ := filepath.Glob(filepath.Join(dir, "manager-*.db"))
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)
	return files[len(files)-1]
}

// NowMs is the current time in unix milliseconds. Overridable in tests.
var NowMs = func() int64 { return time.Now().UnixMilli() }
```

### B.8 `internal/store/migrations/001_init.sql`: settings + inbox/outbox pattern

```sql
-- internal/store/migrations/001_init.sql
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL            -- JSON
);

-- Inbox: every incoming event stored once (retries are no-ops via INSERT OR IGNORE).
CREATE TABLE inbox (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source      TEXT NOT NULL,
    event_id    TEXT NOT NULL,
    event       TEXT NOT NULL,
    received_at INTEGER NOT NULL,  -- unix ms
    status      TEXT NOT NULL DEFAULT 'new',   -- new | processed | ignored | error
    note        TEXT NOT NULL DEFAULT '',      -- i18n key explaining the decision
    payload     TEXT NOT NULL,
    UNIQUE(source, event_id, event)
);
CREATE INDEX idx_inbox_status ON inbox(status, id);

-- Outbox: one row per (event, destination); a destination never receives an event twice.
CREATE TABLE outbox (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    inbox_id        INTEGER NOT NULL,
    source          TEXT NOT NULL,
    event_id        TEXT NOT NULL,
    channel         TEXT NOT NULL,             -- e.g. email | chat
    destination_key TEXT NOT NULL,             -- e.g. mail:a@b.com
    action          TEXT NOT NULL,             -- JSON
    status          TEXT NOT NULL DEFAULT 'pending', -- pending | sending | sent | failed | uncertain | cancelled
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(source, event_id, destination_key)
);
CREATE INDEX idx_outbox_due ON outbox(status, channel, next_attempt_at);

-- IDs of what this app sent, so echoes are ignored (loop guard).
CREATE TABLE sent_ids (
    id         TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL
);

-- Key queries:
--   claim:    BEGIN; SELECT ... WHERE status='pending' AND channel=? AND next_attempt_at<=? ORDER BY next_attempt_at, id LIMIT 1;
--             UPDATE outbox SET status='sending', attempts=attempts+1 WHERE id=? AND status='pending'; COMMIT;
--   startup:  UPDATE outbox SET status='uncertain' WHERE status='sending';
--   counter:  INSERT INTO settings(key,value) VALUES(?,'1') ON CONFLICT(key) DO UPDATE
--             SET value=CAST(CAST(value AS INTEGER)+1 AS TEXT) RETURNING CAST(value AS INTEGER);
```

### B.9 `internal/ui/ui.go` (also commit internal/ui/dist/.gitkeep)

```go
// Package ui embeds the built web interface (web/ → internal/ui/dist).
package ui

import "embed"

// FS holds dist/; it contains only .gitkeep until `npm run build` runs.
//
//go:embed all:dist
var FS embed.FS
```

### B.10 `assets/assets.go`: embedded tray icons

```go
// Package assets embeds generated icons (see tools/genicons).
package assets

import _ "embed"

var (
	//go:embed icons/tray-green.ico
	TrayGreen []byte
	//go:embed icons/tray-yellow.ico
	TrayYellow []byte
	//go:embed icons/tray-red.ico
	TrayRed []byte
	//go:embed icons/tray-gray.ico
	TrayGray []byte
)
```

## Appendix C: Icon generator

### C.1 `tools/genicons/main.go`: run `go run ./tools/genicons`

```go
// Command genicons renders the app, tray, installer and Store icons from code
// so they are reproducible. Run: go run ./tools/genicons
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

var (
	green1 = color.NRGBA{0x2b, 0xb6, 0x73, 0xff}
	green2 = color.NRGBA{0x0f, 0x7a, 0x45, 0xff}
	white  = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

// render draws the logo at size s (supersampled 4x) with an optional status dot.
func render(s int, dot color.Color, pad float64) *image.NRGBA {
	const ss = 4
	S := s * ss
	big := image.NewNRGBA(image.Rect(0, 0, S, S))
	fs := float64(S)
	r := fs * 0.22 // corner radius
	inset := fs * pad
	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			if !inRoundRect(px, py, inset, inset, fs-inset, fs-inset, r*(1-2*pad)) {
				continue
			}
			t := (px + py) / (2 * fs)
			big.SetNRGBA(x, y, lerp(green1, green2, t))
			// Speech bubble.
			cx, cy, br := fs*0.5, fs*0.47, fs*0.30
			inBubble := dist(px, py, cx, cy) <= br
			// Tail (triangle bottom-left).
			if pointInTri(px, py, fs*0.27, fs*0.62, fs*0.40, fs*0.72, fs*0.22, fs*0.82) {
				inBubble = true
			}
			if inBubble {
				big.SetNRGBA(x, y, white)
				// Forward arrow inside the bubble.
				if inArrow(px/fs, py/fs) {
					big.SetNRGBA(x, y, lerp(green1, green2, 0.6))
				}
			}
		}
	}
	if dot != nil {
		dc := color.NRGBAModel.Convert(dot).(color.NRGBA)
		cx, cy, dr := fs*0.78, fs*0.78, fs*0.22
		for y := 0; y < S; y++ {
			for x := 0; x < S; x++ {
				d := dist(float64(x)+0.5, float64(y)+0.5, cx, cy)
				if d <= dr {
					big.SetNRGBA(x, y, white)
				}
				if d <= dr*0.75 {
					big.SetNRGBA(x, y, dc)
				}
			}
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, s, s))
	draw.CatmullRom.Scale(out, out.Bounds(), big, big.Bounds(), draw.Over, nil)
	return out
}

func inArrow(x, y float64) bool {
	// Shaft.
	if x >= 0.36 && x <= 0.55 && y >= 0.435 && y <= 0.505 {
		return true
	}
	// Head.
	return pointInTri(x, y, 0.53, 0.36, 0.53, 0.58, 0.66, 0.47)
}

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return dist(x, y, cx, cy) <= r
}

func dist(x, y, a, b float64) float64 { return math.Hypot(x-a, y-b) }

func pointInTri(px, py, ax, ay, bx, by, cx, cy float64) bool {
	s := func(x1, y1, x2, y2, x3, y3 float64) float64 { return (x1-x3)*(y2-y3) - (x2-x3)*(y1-y3) }
	d1, d2, d3 := s(px, py, ax, ay, bx, by), s(px, py, bx, by, cx, cy), s(px, py, cx, cy, ax, ay)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 0xff}
}

func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		log.Fatal(err)
	}
	return b.Bytes()
}

// writeICO writes a PNG-compressed multi-size .ico.
func writeICO(path string, imgs []*image.NRGBA) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	var data [][]byte
	for _, im := range imgs {
		d := pngBytes(im)
		data = append(data, d)
		w := im.Bounds().Dx()
		bw := uint8(w)
		if w >= 256 {
			bw = 0
		}
		binary.Write(&buf, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{bw, bw, 0, 0, 1, 32, uint32(len(d)), uint32(offset)})
		offset += len(d)
	}
	for _, d := range data {
		buf.Write(d)
	}
	must(os.WriteFile(path, buf.Bytes(), 0o644))
}

func writePNG(path string, img image.Image) { must(os.WriteFile(path, pngBytes(img), 0o644)) }

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// wide renders the logo centered on a transparent canvas of w×h.
func wide(w, h int) *image.NRGBA {
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	s := h * 7 / 10
	logo := render(s, nil, 0)
	off := image.Pt((w-s)/2, (h-s)/2)
	draw.Draw(canvas, logo.Bounds().Add(off), logo, image.Point{}, draw.Over)
	return canvas
}

// tall renders the logo centered on a brand-green poster of w×h.
func tall(w, h int) *image.NRGBA {
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		c := lerp(green1, green2, float64(y)/float64(h))
		for x := 0; x < w; x++ {
			canvas.SetNRGBA(x, y, c)
		}
	}
	s := w * 6 / 10
	logo := render(s, nil, 0)
	off := image.Pt((w-s)/2, (h-s)/2)
	draw.Draw(canvas, logo.Bounds().Add(off), logo, image.Point{}, draw.Over)
	return canvas
}

func main() {
	root := "assets"
	icons := filepath.Join(root, "icons")
	msix := filepath.Join(root, "msix")
	for _, d := range []string{icons, msix} {
		must(os.MkdirAll(d, 0o755))
	}
	var app []*image.NRGBA
	for _, s := range []int{16, 20, 24, 32, 40, 48, 64, 128, 256} {
		app = append(app, render(s, nil, 0))
	}
	writeICO(filepath.Join(icons, "app.ico"), app)
	writePNG(filepath.Join(icons, "app-256.png"), render(256, nil, 0))
	writePNG(filepath.Join(icons, "app-512.png"), render(512, nil, 0))

	dots := map[string]color.Color{
		"green":  color.NRGBA{0x22, 0xc5, 0x5e, 0xff},
		"yellow": color.NRGBA{0xf5, 0x9e, 0x0b, 0xff},
		"red":    color.NRGBA{0xef, 0x44, 0x44, 0xff},
		"gray":   color.NRGBA{0x9c, 0xa3, 0xaf, 0xff},
	}
	for name, c := range dots {
		writeICO(filepath.Join(icons, "tray-"+name+".ico"), []*image.NRGBA{render(16, c, 0), render(20, c, 0), render(24, c, 0), render(32, c, 0), render(48, c, 0)})
	}

	// MSIX / Store assets (plated, with 1/8 padding like system tiles).
	writePNG(filepath.Join(msix, "Square44x44Logo.png"), render(44, nil, 0))
	writePNG(filepath.Join(msix, "Square44x44Logo.targetsize-24_altform-unplated.png"), render(24, nil, 0))
	writePNG(filepath.Join(msix, "Square44x44Logo.targetsize-48_altform-unplated.png"), render(48, nil, 0))
	writePNG(filepath.Join(msix, "Square44x44Logo.targetsize-256_altform-unplated.png"), render(256, nil, 0))
	writePNG(filepath.Join(msix, "Square150x150Logo.png"), wide(150, 150))
	writePNG(filepath.Join(msix, "Wide310x150Logo.png"), wide(310, 150))
	writePNG(filepath.Join(msix, "StoreLogo.png"), render(50, nil, 0))
	writePNG(filepath.Join(msix, "SplashScreen.png"), wide(620, 300))

	// Partner Center listing artwork (uploaded by hand, not packaged).
	store := filepath.Join(root, "store")
	must(os.MkdirAll(store, 0o755))
	writePNG(filepath.Join(store, "app-icon-300.png"), render(300, nil, 0))
	writePNG(filepath.Join(store, "box-art-1080.png"), render(1080, nil, 0.12))
	writePNG(filepath.Join(store, "poster-720x1080.png"), tall(720, 1080))
	log.Println("icons written to", root)
}
```

## Appendix D: Web UI (Vue 3 + Vite + Tailwind 4 + vue-i18n)

### D.1 `web/package.json`

```json
{
  "name": "myapp-ui",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "lint": "eslint src"
  },
  "dependencies": {
    "@fontsource/ibm-plex-sans-arabic": "^5.3.0",
    "@fontsource/inter": "^5.3.0",
    "vue": "^3.5.44",
    "vue-i18n": "^11.4.16",
    "vue-router": "^5.4.0"
  },
  "devDependencies": {
    "@eslint/js": "^10.0.1",
    "@tailwindcss/vite": "^4.3.3",
    "@vitejs/plugin-vue": "^6.0.10",
    "eslint": "^10.13.0",
    "eslint-plugin-vue": "^10.11.1",
    "globals": "^17.13.0",
    "tailwindcss": "^4.3.3",
    "vite": "^8.3.4"
  }
}
```

### D.2 `web/vite.config.js`: builds into internal/ui/dist

```js
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

const outDir = resolve(__dirname, '../internal/ui/dist')

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    {
      // Keep the placeholder so `go build` works on a clean checkout.
      name: 'keep-gitkeep',
      closeBundle() { writeFileSync(resolve(outDir, '.gitkeep'), '') },
    },
  ],
  base: './',
  build: {
    outDir,
    emptyOutDir: true,
    assetsInlineLimit: 0,
    chunkSizeWarningLimit: 1500,
  },
  server: {
    // `npm run dev` proxies the API to a running app (set MYAPP_API).
    proxy: { '/api': process.env.MYAPP_API || 'http://127.0.0.1:39200' },
  },
})
```

### D.3 `web/eslint.config.js`

```js
import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import globals from 'globals'

export default [
  js.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    languageOptions: { globals: { ...globals.browser } },
    rules: {
      'vue/multi-word-component-names': 'off',
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/no-v-html': 'off',
    },
  },
]
```

### D.4 `web/index.html`

```html
<!doctype html>
<html lang="ar" dir="rtl">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <link rel="icon" type="image/png" href="./favicon.png" />
    <title>My App</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.js"></script>
  </body>
</html>
```

### D.5 `web/src/style.css`: Tailwind 4 theme, dark mode, @utility components

```css
@import "tailwindcss";

@custom-variant dark (&:where(.dark, .dark *));

@theme {
  --font-sans: "IBM Plex Sans Arabic", "Inter", "Segoe UI", Tahoma, sans-serif;
  --color-brand-50: #ecfdf3;
  --color-brand-100: #d1fae1;
  --color-brand-200: #a6f2c6;
  --color-brand-300: #6de3a3;
  --color-brand-400: #34c97c;
  --color-brand-500: #1faf66;
  --color-brand-600: #138d51;
  --color-brand-700: #117043;
  --color-brand-800: #125938;
  --color-brand-900: #10492f;
}

html:lang(en) {
  --font-sans: "Inter", "IBM Plex Sans Arabic", "Segoe UI", sans-serif;
}

html, body, #app { height: 100%; }
body {
  @apply bg-slate-50 text-slate-800 antialiased dark:bg-slate-950 dark:text-slate-100;
  font-family: var(--font-sans);
}

@utility card {
  @apply rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900;
}
@utility btn {
  @apply inline-flex items-center justify-center gap-2 rounded-xl px-4 py-2 text-sm font-semibold transition select-none disabled:cursor-not-allowed disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-400;
}
@utility btn-primary {
  @apply btn bg-brand-600 text-white hover:bg-brand-700;
}
@utility btn-secondary {
  @apply btn border border-slate-300 bg-white text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700;
}
@utility btn-ghost {
  @apply btn text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800;
}
@utility btn-danger {
  @apply btn bg-red-600 text-white hover:bg-red-700;
}
@utility btn-sm {
  @apply px-3 py-1.5 text-xs rounded-lg;
}
@utility input {
  @apply w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 focus:border-brand-500 focus:outline-none focus:ring-2 focus:ring-brand-200 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100 dark:focus:ring-brand-900;
}
@utility label {
  @apply mb-1 block text-sm font-medium text-slate-700 dark:text-slate-300;
}
@utility hint {
  @apply mt-1 text-xs text-slate-500 dark:text-slate-400;
}
@utility badge {
  @apply inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-semibold;
}
@utility badge-green {
  @apply badge bg-brand-100 text-brand-800 dark:bg-brand-900 dark:text-brand-200;
}
@utility badge-yellow {
  @apply badge bg-amber-100 text-amber-800 dark:bg-amber-900/50 dark:text-amber-200;
}
@utility badge-red {
  @apply badge bg-red-100 text-red-800 dark:bg-red-900/50 dark:text-red-200;
}
@utility badge-gray {
  @apply badge bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300;
}
@utility badge-blue {
  @apply badge bg-sky-100 text-sky-800 dark:bg-sky-900/50 dark:text-sky-200;
}
@utility section-title {
  @apply text-lg font-bold text-slate-900 dark:text-white;
}
@utility choice {
  @apply cursor-pointer rounded-xl border-2 border-slate-200 bg-white p-3 text-start transition hover:border-brand-300 dark:border-slate-700 dark:bg-slate-900;
}
@utility choice-on {
  @apply border-brand-500 bg-brand-50 dark:bg-brand-900/30;
}

/* Numbers and phone numbers keep LTR order inside RTL text. */
.ltr { direction: ltr; unicode-bidi: isolate; }
```

### D.6 `web/src/i18n.js`

```js
import { createI18n } from 'vue-i18n'
import ar from './locales/ar.json'
import en from './locales/en.json'

export const i18n = createI18n({
  legacy: false,
  locale: 'ar',
  fallbackLocale: 'en',
  messages: { ar, en },
  missingWarn: false,
  fallbackWarn: false,
})

export function setLocale(lang) {
  const l = lang === 'en' ? 'en' : 'ar'
  i18n.global.locale.value = l
  document.documentElement.lang = l
  document.documentElement.dir = l === 'ar' ? 'rtl' : 'ltr'
}

// errText translates an API error key, falling back to a generic message.
export function errText(key) {
  const { t, te } = i18n.global
  if (key && te(key)) return t(key)
  if (key && te(key, 'en')) return t(key, {}, { locale: 'en' })
  return t('errors.unknown')
}

export function applyTheme(theme) {
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
}
```

### D.7 `web/src/api.js`: token capture + fetch wrapper

```js
// Thin client for the local API. The per-launch token arrives once in the
// URL (#/?t=...) and is kept in sessionStorage for reloads.

const KEY = 'wam_token'

function captureToken() {
  const m = /[?&]t=([a-f0-9]+)/i.exec(window.location.hash || '')
  if (m) {
    try { sessionStorage.setItem(KEY, m[1]) } catch { /* storage unavailable */ }
    return m[1]
  }
  try { return sessionStorage.getItem(KEY) || '' } catch { return '' }
}

const token = captureToken()

export class ApiError extends Error {
  constructor(status, key, detail) {
    super(key || `HTTP ${status}`)
    this.status = status
    this.key = key || 'errors.unknown'
    this.detail = detail || ''
  }
}

export async function api(method, path, body) {
  let res
  try {
    res = await fetch(path, {
      method,
      headers: {
        'X-Auth-Token': token,
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch (e) {
    throw new ApiError(0, 'errors.app_unreachable', String(e))
  }
  const text = await res.text()
  let data
  try { data = text ? JSON.parse(text) : null } catch { data = null }
  if (!res.ok) {
    throw new ApiError(res.status, data && data.error, data && data.detail)
  }
  return data
}

export const get = (p) => api('GET', p)
export const post = (p, b = {}) => api('POST', p, b)
export const put = (p, b) => api('PUT', p, b)
export const del = (p) => api('DELETE', p)

// downloadUrl returns a URL for file downloads (token in the query string).
export function downloadUrl(path) {
  return `${path}${path.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}`
}

export const hasToken = () => !!token
```

### D.8 `web/src/state.js`

```js
import { reactive } from 'vue'
import { get, ApiError } from './api'

// Global app state, refreshed by polling /api/state.
export const app = reactive({
  loaded: false,
  error: '',
  version: '',
  general: { language: 'en', theme: 'system' },
})

let timer = null
export async function refreshState() {
  try {
    Object.assign(app, await get('/api/state'), { loaded: true, error: '' })
  } catch (e) {
    app.error = e instanceof ApiError ? e.key : 'errors.unknown'
  }
}
export function startPolling() {
  if (timer) return
  refreshState()
  timer = setInterval(refreshState, 2500)
}
```

### D.9 `web/src/main.js`

```js
import { createApp, watch } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import '@fontsource/ibm-plex-sans-arabic/400.css'
import '@fontsource/ibm-plex-sans-arabic/600.css'
import '@fontsource/inter/400.css'
import '@fontsource/inter/600.css'
import './style.css'

import App from './App.vue'
import { i18n, setLocale, applyTheme } from './i18n'
import { app as state, startPolling } from './state' // reactive store polling GET /api/state

import Dashboard from './views/Dashboard.vue'
import Settings from './views/Settings.vue'

const router = createRouter({
  history: createWebHashHistory(), // hash routes: the server only serves "/"
  routes: [
    { path: '/', component: Dashboard },
    { path: '/settings/:tab?', component: Settings, props: true },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// Strip the token from the visible URL once captured by api.js.
router.beforeEach((to) => {
  if (to.query && to.query.t) {
    const q = { ...to.query }
    delete q.t
    return { path: to.path, query: q, replace: true }
  }
})

setLocale(state.general.language)
applyTheme(state.general.theme)
watch(() => state.general.language, (l) => setLocale(l))
watch(() => state.general.theme, (t) => applyTheme(t))
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => applyTheme(state.general.theme))

startPolling()
createApp(App).use(i18n).use(router).mount('#app')
```

### D.10 `web/src/locales/en.json`

```json
{
  "app": { "name": "My App", "tagline": "Replace with one sentence about your app." },
  "nav": { "dashboard": "Home", "settings": "Settings" },
  "common": { "save": "Save", "cancel": "Cancel", "saved": "Saved", "language": "Language" },
  "dashboard": { "title": "Home", "version": "Version {v}" },
  "settings": { "title": "Settings" },
  "errors": {
    "unknown": "Something went wrong. Please try again.",
    "unauthorized": "Session expired. Close this window and open the app again from the tray.",
    "app_unreachable": "Cannot reach the app service."
  }
}
```

### D.11 `web/src/locales/ar.json`: always the same keys as en.json

```json
{
  "app": { "name": "تطبيقي", "tagline": "ضع هنا جملة واحدة تصف التطبيق." },
  "nav": { "dashboard": "الرئيسية", "settings": "الإعدادات" },
  "common": { "save": "حفظ", "cancel": "إلغاء", "saved": "تم الحفظ", "language": "اللغة" },
  "dashboard": { "title": "الرئيسية", "version": "الإصدار {v}" },
  "settings": { "title": "الإعدادات" },
  "errors": {
    "unknown": "حدث خطأ ما. يرجى المحاولة مرة أخرى.",
    "unauthorized": "انتهت الجلسة. أغلق هذه النافذة وافتح التطبيق مجدداً من شريط المهام.",
    "app_unreachable": "تعذّر الوصول إلى خدمة التطبيق."
  }
}
```

### D.12 `web/src/App.vue`: shell with sidebar (logo.png = copy of assets/icons/app-256.png)

```vue
<script setup>
import { useRoute } from 'vue-router'
import { app } from './state'
import logo from './logo.png'

const route = useRoute()
const nav = [
  { to: '/', label: 'nav.dashboard' },
  { to: '/settings', label: 'nav.settings' },
]
const isActive = (to) => (to === '/' ? route.path === '/' : route.path.startsWith(to))
</script>

<template>
  <div class="flex h-full">
    <aside class="flex w-60 shrink-0 flex-col overflow-y-auto border-e border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
      <div class="flex items-center gap-3 px-5 py-5">
        <img :src="logo" alt="" class="h-9 w-9 rounded-xl" />
        <div>
          <div class="whitespace-nowrap text-[15px] font-bold leading-tight">{{ $t('app.name') }}</div>
          <div class="ltr text-xs text-slate-500">v{{ app.version }}</div>
        </div>
      </div>
      <nav class="flex-1 space-y-1 px-3">
        <router-link
          v-for="n in nav"
          :key="n.to"
          :to="n.to"
          class="flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium"
          :class="isActive(n.to) ? 'bg-brand-50 text-brand-700 dark:bg-brand-900/40 dark:text-brand-200' : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'"
        >
          {{ $t(n.label) }}
        </router-link>
      </nav>
    </aside>
    <main class="min-w-0 flex-1 overflow-y-auto">
      <p v-if="app.error" class="m-6 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-900/30 dark:text-red-200">
        {{ $t(app.error) }}
      </p>
      <router-view />
    </main>
  </div>
</template>
```

### D.13 `web/src/views/Dashboard.vue`

```vue
<script setup>
import { app } from '../state'
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-6 p-6">
    <h1 class="text-2xl font-bold">{{ $t('dashboard.title') }}</h1>
    <div class="card p-6">
      <p>{{ $t('app.tagline') }}</p>
      <p class="mt-2 text-sm text-slate-500">{{ $t('dashboard.version', { v: app.version }) }}</p>
    </div>
  </div>
</template>
```

### D.14 `web/src/views/Settings.vue`

```vue
<script setup>
import { app } from '../state'
import { setLocale } from '../i18n'

// Replace with PUT /api/settings so the choice is saved by the Go side.
function choose(lang) {
  app.general.language = lang
  setLocale(lang)
}
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-6 p-6">
    <h1 class="text-2xl font-bold">{{ $t('settings.title') }}</h1>
    <div class="card space-y-3 p-6">
      <div class="label">{{ $t('common.language') }}</div>
      <div class="flex gap-2">
        <button class="choice !py-2 font-semibold" :class="app.general.language === 'ar' && 'choice-on'" @click="choose('ar')">العربية</button>
        <button class="choice !py-2 font-semibold" :class="app.general.language === 'en' && 'choice-on'" @click="choose('en')">English</button>
      </div>
    </div>
  </div>
</template>
```

### D.15 `web/go.mod`

```go
// web/go.mod: exists only so that Go tooling (./...) skips node_modules.
module myapp/web

go 1.25
```

## Appendix E: Build

### E.1 `build.ps1`

```powershell
<#
.SYNOPSIS
  Builds My App: web UI, Go exe, portable zip, Inno Setup installer and (optionally) MSIX.

.EXAMPLE
  ./build.ps1                         # dev build (exe + portable zip)
  ./build.ps1 -Version 1.2.0 -Installer -Msix -UpdateRepo owner/repo
#>
param(
    [string]$Version = '',
    [ValidateSet('github', 'store')][string]$Channel = 'github',
    [string]$UpdateRepo = '',
    [switch]$Installer,
    [switch]$Msix,
    [switch]$SkipUI,
    [switch]$SkipTests
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

function Step($m) { Write-Host "`n==> $m" -ForegroundColor Cyan }
function Exec([scriptblock]$cmd) { & $cmd; if ($LASTEXITCODE -ne 0) { throw "Command failed ($LASTEXITCODE): $cmd" } }

if (-not $Version) {
    $tag = (git describe --tags --abbrev=0 2>$null)
    $Version = if ($tag) { $tag.TrimStart('v') } else { '0.0.0' }
}
$Version = $Version.TrimStart('v')
if ($Version -notmatch '^\d+\.\d+\.\d+') { throw "Version must be SemVer (got '$Version')" }
$numeric = ($Version -split '[-+]')[0]
$fileVersion = "$numeric.0"
Write-Host "My App $Version ($Channel)"

$dist = Join-Path $root 'dist'
$stage = Join-Path $dist 'app'
if (Test-Path $dist) { Remove-Item -Recurse -Force $dist }
New-Item -ItemType Directory -Force $stage | Out-Null

if (-not $SkipUI) {
    Step 'Building web UI'
    Push-Location web
    if (-not (Test-Path node_modules)) { Exec { npm ci } }
    Exec { npm run build }
    Pop-Location
}

# Optional bundled helper programs: one build/<name>.version file each (see fetch-helper.ps1).
Get-ChildItem build -Filter '*.version' -ErrorAction SilentlyContinue | ForEach-Object {
    Step "Fetching helper $($_.BaseName)"
    Exec { powershell -NoProfile -ExecutionPolicy Bypass -File build/fetch-helper.ps1 -VersionFile $_.FullName }
}

if (-not $SkipTests) {
    Step 'Running tests'
    Exec { go vet ./... }
    Exec { go test ./... }
}

Step 'Embedding Windows resources'
$winres = Join-Path (go env GOPATH) 'bin/go-winres.exe'
if (-not (Test-Path $winres)) { Exec { go install github.com/tc-hib/go-winres@latest } }
Exec { & $winres make --in build/winres/winres.json --out cmd/myapp/rsrc --arch amd64 --product-version $fileVersion --file-version $fileVersion }

Step 'Building MyApp.exe'
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$ldflags = "-s -w -H windowsgui -X main.version=$Version -X main.channel=$Channel -X main.updateRepo=$UpdateRepo"
Exec { go build -trimpath -ldflags $ldflags -o (Join-Path $stage 'MyApp.exe') ./cmd/myapp }

Step 'Staging files'
New-Item -ItemType Directory -Force (Join-Path $stage 'LICENSES') | Out-Null
if (Test-Path build/bin) { Copy-Item build/bin/* $stage -Recurse }   # fetched helpers: build/bin/<name>/<exe>
Copy-Item LICENSE (Join-Path $stage 'LICENSE.txt')
if (Test-Path THIRD_PARTY_NOTICES.md) { Copy-Item THIRD_PARTY_NOTICES.md (Join-Path $stage 'THIRD_PARTY_NOTICES.md') }
if (Test-Path LICENSES) { Copy-Item LICENSES/* (Join-Path $stage 'LICENSES') }

Step 'Creating portable zip'
$zip = Join-Path $dist "MyApp-$Version-portable.zip"
Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $zip

if ($Installer) {
    Step 'Building installer (Inno Setup)'
    $iscc = @(
        (Get-Command ISCC.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source),
        "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
    ) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
    if (-not $iscc) { throw 'Inno Setup 6 (ISCC.exe) not found. Install it: winget install JRSoftware.InnoSetup' }
    Exec { & $iscc "/DAppVersion=$Version" "/DNumericVersion=$fileVersion" "/DSourceDir=$stage" "/DOutputDir=$dist" build/inno/setup.iss }
}

if ($Msix) {
    Step 'Building MSIX'
    Exec { powershell -NoProfile -ExecutionPolicy Bypass -File build/msix/build-msix.ps1 -Version $fileVersion -SourceDir $stage -OutputDir $dist }
}

Step 'Checksums'
$sums = Get-ChildItem $dist -File | Where-Object { $_.Extension -in '.zip', '.exe', '.msix' } | ForEach-Object {
    "$((Get-FileHash -Algorithm SHA256 $_.FullName).Hash.ToLower())  $($_.Name)"
}
$sums | Set-Content -Encoding ascii (Join-Path $dist 'checksums.txt')
$sums | ForEach-Object { Write-Host $_ }
Write-Host "`nDone. Output: $dist" -ForegroundColor Green
```

### E.2 `build/fetch-helper.ps1` (optional bundled programs)

```powershell
# Downloads one pinned helper program, verifies its SHA-256 and extracts
# its .exe to build/bin/<NAME>/<EXE>. Safe to re-run (cached).
# Usage: fetch-helper.ps1 -VersionFile build/<name>.version
param([Parameter(Mandatory)][string]$VersionFile)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$cfg = @{}
Get-Content $VersionFile | Where-Object { $_ -match '^\s*([A-Z0-9_]+)=(.+)$' } | ForEach-Object { $cfg[$Matches[1]] = $Matches[2].Trim() }
$out = Join-Path $root "bin\$($cfg.NAME)"
$exe = Join-Path $out $cfg.EXE
$stamp = Join-Path $root "bin\$($cfg.NAME).sha256"
if ((Test-Path $exe) -and (Test-Path $stamp) -and ((Get-Content $stamp -Raw).Trim() -eq $cfg.SHA256)) { Write-Host "$($cfg.NAME) $($cfg.VERSION) already present"; exit 0 }
New-Item -ItemType Directory -Force $out | Out-Null
$zip = Join-Path $root "bin\$($cfg.NAME).zip"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Invoke-WebRequest -Uri $cfg.URL -OutFile $zip -UseBasicParsing
$hash = (Get-FileHash -Algorithm SHA256 $zip).Hash.ToLower()
if ($hash -ne $cfg.SHA256.ToLower()) { Remove-Item $zip -Force; throw "SHA-256 mismatch for $($cfg.URL): got $hash" }
$tmp = Join-Path $root "bin\$($cfg.NAME)-extract"
if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp }
Expand-Archive -Path $zip -DestinationPath $tmp
$bin = Get-ChildItem -Path $tmp -Filter '*.exe' -Recurse | Select-Object -First 1
if (-not $bin) { throw 'No .exe in archive' }
Copy-Item $bin.FullName $exe -Force
Remove-Item -Recurse -Force $tmp, $zip
Set-Content -Path $stamp -Value $cfg.SHA256 -Encoding ascii
Write-Host "$($cfg.NAME) $($cfg.VERSION) ready at $exe"
```

### E.3 `build/somehelper.version.example` (rename to .version to use)

```ini
# build/somehelper.version: one file per bundled helper program (optional).
# Update: change VERSION/URL, then copy the SHA-256 from the release's checksums.txt.
NAME=somehelper
VERSION=1.2.3
EXE=somehelper.exe
URL=https://github.com/OWNER/somehelper/releases/download/v1.2.3/somehelper_1.2.3_windows_amd64.zip
SHA256=0000000000000000000000000000000000000000000000000000000000000000
```

### E.4 `build/winres/winres.json`: exe icon, manifest, version info

```json
{
  "RT_GROUP_ICON": {
    "#1": { "0000": "../../assets/icons/app.ico" }
  },
  "RT_MANIFEST": {
    "#1": {
      "0409": {
        "identity": { "name": "MyApp", "version": "" },
        "description": "My App",
        "minimum-os": "win10",
        "execution-level": "as invoker",
        "ui-access": false,
        "auto-elevate": false,
        "dpi-awareness": "per monitor v2",
        "disable-theming": false,
        "disable-window-filtering": false,
        "high-resolution-scrolling-aware": true,
        "ultra-high-resolution-scrolling-aware": false,
        "long-path-aware": true,
        "printer-driver-isolation": false,
        "gdi-scaling": false,
        "segment-heap": false,
        "use-common-controls-v6": true
      }
    }
  },
  "RT_VERSION": {
    "#1": {
      "0000": {
        "fixed": { "file_version": "0.0.0.0", "product_version": "0.0.0.0" },
        "info": {
          "0409": {
            "CompanyName": "Your Company",
            "FileDescription": "My App",
            "FileVersion": "0.0.0",
            "InternalName": "myapp",
            "LegalCopyright": "MIT License",
            "OriginalFilename": "MyApp.exe",
            "ProductName": "My App",
            "ProductVersion": "0.0.0"
          }
        }
      }
    }
  }
}
```

## Appendix F: Installer and Store package

### F.1 `build/inno/setup.iss`: per-user installer, Arabic + English

```ini
; Inno Setup script for My App (per-user install, no admin rights).
; Built by build.ps1:  ISCC /DAppVersion=1.2.3 /DNumericVersion=1.2.3.0 /DSourceDir=dist\app /DOutputDir=dist setup.iss

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef NumericVersion
  #define NumericVersion "0.0.0.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\..\dist\app"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif

#define AppName "My App"
#define AppExe "MyApp.exe"
#define AppUserModelID "MyApp.Desktop"

[Setup]
AppId={{NEW-GUID}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=Your Company
AppPublisherURL=https://example.com/
AppSupportURL=https://example.com/
AppContact=support@example.com
VersionInfoVersion={#NumericVersion}
VersionInfoProductName={#AppName}
DefaultDirName={userpf}\MyApp
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
DisableDirPage=yes
DisableReadyPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.17763
OutputDir={#OutputDir}
OutputBaseFilename=MyApp-Setup-{#AppVersion}
SetupIconFile=..\..\assets\icons\app.ico
UninstallDisplayIcon={app}\{#AppExe}
UninstallDisplayName={#AppName}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
CloseApplications=force
RestartApplications=no
ShowLanguageDialog=auto
LicenseFile=..\..\LICENSE

[Languages]
Name: "arabic"; MessagesFile: "compiler:Languages\Arabic.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
arabic.DesktopIcon=إنشاء اختصار على سطح المكتب
english.DesktopIcon=Create a desktop shortcut
arabic.LaunchApp=تشغيل My App
english.LaunchApp=Launch My App
arabic.RemoveData=هل تريد حذف الإعدادات والبيانات المحفوظة أيضاً؟%n%nاختر «لا» إذا كنت ستعيد التثبيت لاحقاً.
english.RemoveData=Also delete settings and saved data?%n%nChoose "No" if you plan to reinstall.
arabic.NoWebView=لم يتم العثور على Microsoft Edge WebView2 Runtime. سيعمل التطبيق داخل المتصفح بدلاً من نافذته الخاصة. يمكنك تثبيته لاحقاً من موقع مايكروسوفت.
english.NoWebView=Microsoft Edge WebView2 Runtime was not found. The app will open in your browser instead of its own window. You can install WebView2 later from Microsoft.

[Tasks]
Name: "desktopicon"; Description: "{cm:DesktopIcon}"; Flags: unchecked

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{userprograms}\{#AppName}"; Filename: "{app}\{#AppExe}"; AppUserModelID: "{#AppUserModelID}"
Name: "{userdesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; Tasks: desktopicon

[Registry]
; The app manages its own autostart entry; make sure uninstall removes it.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueName: "MyApp"; ValueType: none; Flags: uninsdeletevalue dontcreatekey

[Run]
Filename: "{app}\{#AppExe}"; Description: "{cm:LaunchApp}"; Flags: nowait postinstall skipifsilent

[Code]
procedure KillApp();
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /T /IM {#AppExe}', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(800);
end;

function WebView2Installed(): Boolean;
var
  V: String;
begin
  Result :=
    (RegQueryStringValue(HKLM, 'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', V) and (V <> '') and (V <> '0.0.0.0')) or
    (RegQueryStringValue(HKLM, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', V) and (V <> '') and (V <> '0.0.0.0')) or
    (RegQueryStringValue(HKCU, 'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', V) and (V <> '') and (V <> '0.0.0.0'));
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  KillApp();
  Result := '';
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and (not WizardSilent()) and (not WebView2Installed()) then
    MsgBox(CustomMessage('NoWebView'), mbInformation, MB_OK);
end;

function InitializeUninstall(): Boolean;
begin
  KillApp();
  Result := True;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: String;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    DataDir := ExpandConstant('{localappdata}\MyApp');
    if DirExists(DataDir) and (not UninstallSilent()) then
      if MsgBox(CustomMessage('RemoveData'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
        DelTree(DataDir, True, True, True);
  end;
end;
```

### F.2 `build/msix/AppxManifest.xml`: template with placeholders

```xml
<?xml version="1.0" encoding="utf-8"?>
<!--
  MSIX manifest for the Microsoft Store build. build-msix.ps1 replaces the
  placeholders. For Store submission, Identity Name/Publisher and
  PublisherDisplayName must match the values Partner Center assigns
  (Product management → Product identity); set them as CI secrets.
-->
<Package
  xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"
  xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10"
  xmlns:uap5="http://schemas.microsoft.com/appx/manifest/uap/windows10/5"
  xmlns:desktop="http://schemas.microsoft.com/appx/manifest/desktop/windows10"
  xmlns:rescap="http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities"
  IgnorableNamespaces="uap uap5 desktop rescap">

  <Identity Name="__IDENTITY_NAME__" Publisher="__PUBLISHER__" Version="__VERSION__" ProcessorArchitecture="x64" />

  <Properties>
    <DisplayName>__DISPLAY_NAME__</DisplayName>
    <PublisherDisplayName>__PUBLISHER_DISPLAY__</PublisherDisplayName>
    <Logo>Assets\StoreLogo.png</Logo>
    <Description>One-sentence description of My App.</Description>
  </Properties>

  <Dependencies>
    <TargetDeviceFamily Name="Windows.Desktop" MinVersion="10.0.17763.0" MaxVersionTested="10.0.26100.0" />
  </Dependencies>

  <Resources>
    <Resource Language="ar-SA" />
    <Resource Language="en-US" />
  </Resources>

  <Applications>
    <Application Id="MyApp" Executable="MyApp.exe" EntryPoint="Windows.FullTrustApplication">
      <uap:VisualElements
        DisplayName="__DISPLAY_NAME__"
        Description="Short description of My App"
        BackgroundColor="transparent"
        Square150x150Logo="Assets\Square150x150Logo.png"
        Square44x44Logo="Assets\Square44x44Logo.png">
        <uap:DefaultTile Wide310x150Logo="Assets\Wide310x150Logo.png" />
        <uap:SplashScreen Image="Assets\SplashScreen.png" />
      </uap:VisualElements>
      <Extensions>
        <!-- Start with Windows (minimized to tray). Users can turn it off in Task Manager → Startup apps. -->
        <desktop:Extension Category="windows.startupTask" Executable="MyApp.exe" EntryPoint="Windows.FullTrustApplication">
          <desktop:StartupTask TaskId="MyAppStartup" Enabled="true" DisplayName="__DISPLAY_NAME__" />
        </desktop:Extension>
        <uap5:Extension Category="windows.appExecutionAlias" Executable="MyApp.exe" EntryPoint="Windows.FullTrustApplication">
          <uap5:AppExecutionAlias>
            <uap5:ExecutionAlias Alias="MyApp.exe" />
          </uap5:AppExecutionAlias>
        </uap5:Extension>
      </Extensions>
    </Application>
  </Applications>

  <Capabilities>
    <Capability Name="internetClient" />
    <Capability Name="privateNetworkClientServer" />
    <rescap:Capability Name="runFullTrust" />
  </Capabilities>
</Package>
```

### F.3 `build/msix/build-msix.ps1`

```powershell
<#
.SYNOPSIS
  Packs the staged app into an MSIX for the Microsoft Store.

  Identity values come from environment variables (CI secrets) and default to
  development placeholders:
    MSIX_IDENTITY_NAME      e.g. 12345Publisher.MyApp
    MSIX_PUBLISHER          e.g. CN=XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX
    MSIX_PUBLISHER_DISPLAY  e.g. Your Name

  The Store signs the package on ingestion. For local sideload testing, sign
  it with a self-signed certificate (see docs/RELEASING.md).
#>
param(
    [Parameter(Mandatory)][string]$Version,   # 4-part numeric, last part must be 0 for the Store
    [Parameter(Mandatory)][string]$SourceDir,
    [Parameter(Mandatory)][string]$OutputDir
)
$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$root = Resolve-Path (Join-Path $here '..\..')

function Find-SdkTool($name) {
    $cmd = Get-Command $name -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $kits = "${env:ProgramFiles(x86)}\Windows Kits\10\bin"
    if (Test-Path $kits) {
        $hit = Get-ChildItem $kits -Recurse -Filter $name -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\x64\\' } | Sort-Object FullName -Descending | Select-Object -First 1
        if ($hit) { return $hit.FullName }
    }
    throw "$name not found. Install the Windows 10/11 SDK (winget install Microsoft.WindowsSDK.10.0.26100)."
}

# Partner Center identity.
# Order: environment variables > store.local.json (gitignored) > store.json (committed).
$local = @{}
foreach ($name in @('store.json', 'store.local.json')) {
    $f = Join-Path $here $name
    if (Test-Path $f) {
        (Get-Content $f -Raw | ConvertFrom-Json).PSObject.Properties | ForEach-Object { if ($_.Value) { $local[$_.Name] = $_.Value } }
        Write-Host "Store identity: $name"
    }
}
function Pick($envName, $key, $default) {
    $v = [Environment]::GetEnvironmentVariable($envName)
    if ($v) { return $v }
    if ($local[$key]) { return $local[$key] }
    return $default
}
$identity = Pick 'MSIX_IDENTITY_NAME' 'identityName' 'MyApp.Dev'
$publisher = Pick 'MSIX_PUBLISHER' 'publisher' 'CN=MyAppDev'
$display = Pick 'MSIX_PUBLISHER_DISPLAY' 'publisherDisplayName' 'My App contributors'
$appName = Pick 'MSIX_DISPLAY_NAME' 'displayName' 'My App'
if ($identity -eq 'MyApp.Dev') { Write-Warning 'Building with DEV identity placeholders: this package cannot be uploaded to the Store.' }

$layout = Join-Path $OutputDir 'msix-layout'
if (Test-Path $layout) { Remove-Item -Recurse -Force $layout }
New-Item -ItemType Directory -Force $layout, (Join-Path $layout 'Assets') | Out-Null
Copy-Item (Join-Path $SourceDir '*') $layout -Recurse
Copy-Item (Join-Path $root 'assets\msix\*') (Join-Path $layout 'Assets')

$manifest = Get-Content (Join-Path $here 'AppxManifest.xml') -Raw
$manifest = $manifest.Replace('__IDENTITY_NAME__', $identity).Replace('__PUBLISHER__', $publisher).
    Replace('__PUBLISHER_DISPLAY__', [Security.SecurityElement]::Escape($display)).Replace('__VERSION__', $Version).
    Replace('__DISPLAY_NAME__', [Security.SecurityElement]::Escape($appName))
[IO.File]::WriteAllText((Join-Path $layout 'AppxManifest.xml'), $manifest, (New-Object Text.UTF8Encoding $false))

$makeappx = Find-SdkTool 'makeappx.exe'
$out = Join-Path $OutputDir "MyApp-$Version.msix"
& $makeappx pack /o /d $layout /p $out
if ($LASTEXITCODE -ne 0) { throw "makeappx failed ($LASTEXITCODE)" }
Remove-Item -Recurse -Force $layout
Write-Host "MSIX: $out"
```

### F.4 `build/msix/store.json` (fill from Partner Center)

```json
{
  "_comment": "Microsoft Store identity (Partner Center > your app > Product management > Product identity). Not secret. Override with store.local.json or MSIX_* env vars.",
  "identityName": "12345YourCompany.myapp",
  "publisher": "CN=XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX",
  "publisherDisplayName": "Your Company",
  "displayName": "My App",
  "storeId": "9XXXXXXXXXXX"
}
```

## Appendix G: GitHub Actions

### G.1 `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
  workflow_dispatch:
    inputs:
      upload:
        description: 'Keep the built installer as a downloadable artifact (uses Actions storage for 3 days)'
        type: boolean
        default: false

permissions:
  contents: read

concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true

jobs:
  build:
    runs-on: windows-latest
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: web/package-lock.json

      - name: Web UI (lint + build)
        working-directory: web
        run: |
          npm ci
          npm run lint
          npm run build

      - name: Go checks (gofmt, vet, tests)
        run: |
          $fmt = gofmt -l cmd internal tools assets
          if ($fmt) { Write-Error "gofmt needed: $fmt"; exit 1 }
          go vet ./...
          go test ./...

      - name: Install Inno Setup
        run: |
          if (-not (Test-Path "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe")) {
            choco install innosetup --no-progress -y
          }

      - name: Build exe, portable zip and installer
        run: ./build.ps1 -SkipUI -SkipTests -Installer -Version 0.0.0-ci

      # Artifacts count against the account's Actions storage, so they are
      # only kept when requested on a manual run.
      - uses: actions/upload-artifact@v4
        if: ${{ github.event_name == 'workflow_dispatch' && inputs.upload }}
        with:
          name: myapp-ci-${{ github.sha }}
          path: |
            dist/*.exe
            dist/*.zip
            dist/checksums.txt
          retention-days: 3
```

### G.2 `.github/workflows/release.yml`

```yaml
name: Release

# Push a tag like v1.2.0 to publish a GitHub Release with the installer,
# the portable zip and checksums. Tags with a "-" (v1.3.0-beta.1) are
# published as pre-releases.
on:
  push:
    tags: ['v*.*.*']

permissions:
  contents: write

concurrency:
  group: release-${{ github.ref }}
  cancel-in-progress: false

jobs:
  release:
    runs-on: windows-latest
    timeout-minutes: 45
    environment: release
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: web/package-lock.json

      - name: Install Inno Setup
        run: |
          if (-not (Test-Path "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe")) {
            choco install innosetup --no-progress -y
          }

      - name: Build (tests included)
        run: ./build.ps1 -Version "${{ github.ref_name }}" -Channel github -UpdateRepo "${{ github.repository }}" -Installer

      # Free code signing for open-source projects via SignPath Foundation.
      # Skipped until the repository variables/secrets below exist
      # (see docs/RELEASING.md).
      - name: Upload unsigned files for signing
        id: unsigned
        if: ${{ vars.SIGNPATH_ORGANIZATION_ID != '' }}
        uses: actions/upload-artifact@v4
        with:
          name: unsigned
          path: dist/*.exe
          retention-days: 1

      - name: Sign with SignPath
        if: ${{ vars.SIGNPATH_ORGANIZATION_ID != '' }}
        uses: signpath/github-action-submit-signing-request@v1
        with:
          api-token: ${{ secrets.SIGNPATH_API_TOKEN }}
          organization-id: ${{ vars.SIGNPATH_ORGANIZATION_ID }}
          project-slug: ${{ vars.SIGNPATH_PROJECT_SLUG }}
          signing-policy-slug: ${{ vars.SIGNPATH_POLICY_SLUG }}
          github-artifact-id: ${{ steps.unsigned.outputs.artifact-id }}
          wait-for-completion: true
          output-artifact-directory: dist/signed

      - name: Use signed files and refresh checksums
        if: ${{ vars.SIGNPATH_ORGANIZATION_ID != '' }}
        run: |
          Copy-Item dist/signed/*.exe dist/ -Force
          Get-ChildItem dist -File | Where-Object { $_.Extension -in '.zip', '.exe' } | ForEach-Object {
            "$((Get-FileHash -Algorithm SHA256 $_.FullName).Hash.ToLower())  $($_.Name)"
          } | Set-Content -Encoding ascii dist/checksums.txt

      - name: Publish GitHub release
        env:
          GH_TOKEN: ${{ github.token }}
          TAG: ${{ github.ref_name }}
        run: |
          $files = @(Get-ChildItem dist -File | Where-Object { $_.Extension -in '.exe', '.zip' -or $_.Name -eq 'checksums.txt' } | ForEach-Object { $_.FullName })
          $ghArgs = @('release', 'create', $env:TAG) + $files + @('--title', "My App $env:TAG", '--generate-notes')
          if ($env:TAG -match '-') { $ghArgs += '--prerelease' }
          gh @ghArgs
          if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
```

### G.3 `.github/workflows/store.yml`

```yaml
name: Microsoft Store

# Builds the MSIX and submits it to the Microsoft Store (product
# "whats-manager", Store ID in build/msix/store.json).
#
# Runs automatically after every stable GitHub release (tag vX.Y.Z), or
# manually from the Actions tab. Requirements (see docs/STORE_LISTING.md):
#   - the first submission was made by hand in Partner Center;
#   - repository variable STORE_ENABLED = true;
#   - secrets in the "store" environment: STORE_TENANT_ID, STORE_SELLER_ID,
#     STORE_CLIENT_ID, STORE_CLIENT_SECRET (a Microsoft Entra app added to
#     Partner Center with the Manager role).
on:
  workflow_run:
    workflows: [Release]
    types: [completed]
  workflow_dispatch:
    inputs:
      version:
        description: 'Version to publish, without "v" (must be higher than the last Store version, e.g. 0.1.1)'
        required: true

permissions:
  contents: read

concurrency:
  group: store
  cancel-in-progress: false

jobs:
  store:
    if: >-
      vars.STORE_ENABLED == 'true' && (
        github.event_name == 'workflow_dispatch' ||
        (github.event.workflow_run.conclusion == 'success' && !contains(github.event.workflow_run.head_branch, '-'))
      )
    runs-on: windows-latest
    timeout-minutes: 45
    environment: store
    steps:
      - name: Resolve version
        id: v
        shell: pwsh
        env:
          INPUT_VERSION: ${{ inputs.version }}
          RUN_TAG: ${{ github.event.workflow_run.head_branch }}
        run: |
          $v = if ($env:INPUT_VERSION) { $env:INPUT_VERSION } else { $env:RUN_TAG }
          $v = $v.TrimStart('v')
          if ($v -notmatch '^\d+\.\d+\.\d+$') { Write-Error "Not a stable version: $v"; exit 1 }
          "version=$v" >> $env:GITHUB_OUTPUT

      - uses: actions/checkout@v4
        with:
          ref: v${{ steps.v.outputs.version }}

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: web/package-lock.json

      - name: Build MSIX (identity from build/msix/store.json)
        run: ./build.ps1 -Version "${{ steps.v.outputs.version }}" -Channel store -Msix -SkipTests

      - name: Read Store ID
        id: store
        shell: pwsh
        run: |
          $cfg = Get-Content build/msix/store.json -Raw | ConvertFrom-Json
          "id=$($cfg.storeId)" >> $env:GITHUB_OUTPUT

      - uses: microsoft/setup-msstore-cli@v1

      - name: Submit to Partner Center
        env:
          STORE_TENANT_ID: ${{ secrets.STORE_TENANT_ID }}
          STORE_SELLER_ID: ${{ secrets.STORE_SELLER_ID }}
          STORE_CLIENT_ID: ${{ secrets.STORE_CLIENT_ID }}
          STORE_CLIENT_SECRET: ${{ secrets.STORE_CLIENT_SECRET }}
          STORE_ID: ${{ steps.store.outputs.id }}
        run: |
          msstore reconfigure --tenantId $env:STORE_TENANT_ID --sellerId $env:STORE_SELLER_ID --clientId $env:STORE_CLIENT_ID --clientSecret $env:STORE_CLIENT_SECRET
          if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
          $msix = Get-ChildItem dist -Filter *.msix | Select-Object -First 1
          msstore publish $msix.FullName -id $env:STORE_ID
          if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
```

### G.4 `.github/workflows/pages.yml`

```yaml
name: Website

# Publishes docs/ to GitHub Pages: docs/index.html is the landing page and
# the Markdown files (user guides, privacy policy) are rendered by Jekyll.
# One-time setup: Settings -> Pages -> Build and deployment -> Source: "GitHub Actions".
# Until that is done this workflow skips (with a notice) instead of failing.
on:
  push:
    branches: [main]
    paths: ['docs/**', '.github/workflows/pages.yml']
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: false

jobs:
  check:
    runs-on: ubuntu-latest
    outputs:
      enabled: ${{ steps.pages.outputs.enabled }}
    steps:
      - name: Is GitHub Pages enabled with "GitHub Actions" as the source?
        id: pages
        env:
          GH_TOKEN: ${{ github.token }}
          REPO: ${{ github.repository }}
        run: |
          if build_type=$(gh api "repos/$REPO/pages" --jq '.build_type' 2>/dev/null) && [ "$build_type" = "workflow" ]; then
            echo "enabled=true" >> "$GITHUB_OUTPUT"
          else
            echo "enabled=false" >> "$GITHUB_OUTPUT"
            echo "::notice title=Website not published::Turn on GitHub Pages: Settings > Pages > Build and deployment > Source: GitHub Actions, then re-run this workflow."
          fi

  build:
    needs: check
    if: needs.check.outputs.enabled == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/configure-pages@v5

      - name: Build with Jekyll
        uses: actions/jekyll-build-pages@v1
        with:
          source: ./docs
          destination: ./_site

      - uses: actions/upload-pages-artifact@v3

  deploy:
    needs: build
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - id: deployment
        uses: actions/deploy-pages@v4
```

### G.5 `.github/dependabot.yml`

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule:
      interval: weekly
    groups:
      go-deps:
        patterns: ['*']

  - package-ecosystem: npm
    directory: /web
    schedule:
      interval: weekly
    groups:
      npm-deps:
        patterns: ['*']

  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: monthly
```

## Appendix H: Agent context files

### H.1 `PROJECT_CONTEXT.md` (template)

```markdown
# Project Context: My App

Durable facts for any agent or developer. Keep it short; update when a fact changes.

## What this is
<one paragraph: who uses it, what it does>

## Fixed decisions
- Windows 10/11 x64 only; per-user install (Inno, no admin) + Microsoft Store (MSIX).
- Go core (CGO off), WebView2 window + tray, Vue/Tailwind UI embedded, SQLite (modernc), DPAPI secrets.
- Data folder: %LOCALAPPDATA%\MyApp (never write next to the exe).
- Default language: <ar|en>; every string in ar.json and en.json.

## Invariants (do not break)
1. Loopback only; UI API needs the per-launch hex token; Host/Origin checked.
2. Inbox UNIQUE(source,event_id,event); outbox UNIQUE(source,event_id,destination_key).
3. Jobs left in `sending` at startup become `uncertain` and are never auto-resent.
4. Secrets only DPAPI-encrypted; never logged, exported or returned by the API.
5. Logical CSS only (ms-/me-/start-/end-); `@` in i18n strings written as {'@'}.

## Dev facts
- Dev run: `go run ./cmd/myapp --data-dir .\devdata` (own mutex, never touches autostart).
- `MYAPP_UI_TOKEN=<hex>` fixes the UI token for scripted tests.
- Build: `./build.ps1 [-Version x.y.z] [-Installer] [-Msix] [-Channel github|store]`.
```

### H.2 `CLAUDE.md`

```markdown
@PROJECT_CONTEXT.md
```

