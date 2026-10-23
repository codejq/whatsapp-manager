# Starter stack: Windows desktop apps for non-technical users

A reusable blueprint taken from **WhatsApp Manager** (`codejq/whatsapp-manager`). Use it as the
standard starting point for any new Windows desktop app that must:

- install with one click and need no setup;
- run in the background (system tray) and start with Windows;
- have a modern web-style interface, in Arabic (RTL) and English;
- ship on GitHub Releases **and** the Microsoft Store from one codebase.

Copy this file into a new project and use the checklist at the end. The versions are the ones this project
shipped with in October 2026; take the newest stable versions when you start.

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
| Email | `github.com/wneessen/go-mail` | SMTP with STARTTLS/SSL/auth types; easy error classification |
| Logs | `log/slog` + `gopkg.in/natefinch/lumberjack.v2` | Rotating files in the data folder |
| Text | `golang.org/x/text` (NFKC, case folding) | Arabic-aware matching |
| Images | `golang.org/x/image/draw` | The icon generator (section 6) |
| Exe resources | `go-winres` (`build/winres/winres.json`) | Icon, version info, DPI-aware manifest |
| Installer | **Inno Setup 6** (per-user, Arabic + English) | One `Setup.exe`, no admin, uninstaller |
| Store package | **MSIX** (`makeappx` from the Windows SDK) | Microsoft signs it; no certificate needed |
| CI/CD | GitHub Actions (`windows-latest`) | Build, test, release, Store submission, website |
| Website | Static `docs/index.html` + Jekyll for Markdown, on GitHub Pages | Free hosting, privacy-policy URL for the Store |
| Signing | SignPath Foundation (free for open source) for GitHub builds; Microsoft for the Store | No paid certificate needed |

**Repo size:** about 130 tracked files. The finished app is about 16 MB, plus any bundled helper binaries.

---

## 2. Architecture pattern

```
MyApp.exe (Go, one process)
 ├─ main: single-instance lock → load settings → start services → tray loop (blocks)
 ├─ tray (fyne systray): status icon, Open / Pause / Language / Quit
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

Rules that kept it robust:

- **Everything listens on loopback only.** The UI API needs a random per-launch token, delivered once in the
  URL fragment and kept in `sessionStorage`. Make the token **hex**, because the SPA's regex expects it.
  The server checks `Host` and `Origin` to stop DNS rebinding and CSRF.
- **All writable data lives in `%LOCALAPPDATA%\<AppName>\`**, never next to the exe. MSIX install folders
  are read-only, and Windows redirects AppData for packaged apps.
- **Single instance:** a named mutex `Local\<AppName>-SingleInstance`. The second launch posts to
  `/ipc/show` (port and token in `instance.json`) and exits. Dev runs with `--data-dir` add a hash of the
  folder to the mutex name, so they can run beside an installed copy.
- **Child processes** go in a Job Object with `KILL_ON_JOB_CLOSE`, and use `CREATE_NO_WINDOW`.
- **WebView2 on its own OS thread** (`runtime.LockOSThread`). Closing the window destroys it (freeing
  memory) while the app keeps running in the tray. Set `DataPath` to the data folder.
- **Autostart:** unpackaged builds use `HKCU\...\Run` with `--minimized`. MSIX builds use a
  `windows.startupTask`, which can't pass arguments, so a packaged launch within 5 minutes of boot is
  treated as an autostart.
- **Packaged detection:** `GetCurrentPackageFullName`. Use it to turn off self-update and registry
  autostart in the Store build.
- **Crash safety:** a top-level `recover` writes `logs\crash-*.txt` and shows a message box. The database
  runs `PRAGMA integrity_check` at start and restores the newest daily backup if it is damaged.

### Reliable background work (inbox → outbox)

For any "react to events and deliver somewhere" app:

1. The receiver only **stores** each event, with a unique key (`UNIQUE(source, id, event)`, using
   `INSERT OR IGNORE`), and returns quickly. Retries become no-ops.
2. A worker turns inbox rows into **outbox jobs in one transaction**, with
   `UNIQUE(source, id, destination)`, so a destination never gets the same item twice.
3. Dispatchers **claim** jobs (`pending → sending`), send, then mark `sent` or `failed`, with backoff
   retries. Failures that aren't the job's fault (offline) don't use up retries.
4. At startup, jobs still in `sending` become **`uncertain`** and are never resent automatically
   (at-most-once). The UI offers "Send again".
5. Record the IDs of what you sent, so echoes are ignored (loop guard).
6. Clean up history after N days (setting), but never delete pending jobs.

---

## 3. Repository layout

```
cmd/<app>/            main.go (flags, single instance, crash handler), desktop.go (tray + window), platform.go
internal/app/         wiring, settings, local API (api.go), maintenance, update checks
internal/store/       SQLite open/migrate/backup + repositories; migrations/*.sql (embedded)
internal/winx/        DPAPI, autostart, mutex, job object, packaged detection, toasts, uptime
internal/<domain>/    pure logic with table-driven tests (rules, engine, dispatch, mail, …)
internal/ui/          //go:embed all:dist  (built UI; dist/.gitkeep committed)
internal/semver/      version comparison
web/                  Vue app (src/views, src/components, src/locales/{ar,en}.json); web/go.mod (see gotchas)
assets/icons|msix|store/  generated images (tools/genicons)
build/                build helpers: fetch-<helper>.ps1 + pinned <helper>.version, winres/, inno/setup.iss, msix/
tools/genicons/       icon generator;  tools/gennotices/  third-party notices generator
docs/                 PLAN.md, RELEASING.md, STORE_LISTING.md, privacy.md, user guides, index.html (website), screenshots/
.github/workflows/    ci.yml, release.yml, store.yml, pages.yml;  dependabot.yml;  ISSUE_TEMPLATE/
build.ps1             one entry point for local and CI builds
PROJECT_CONTEXT.md    durable facts and invariants for any agent;  CLAUDE.md → "@PROJECT_CONTEXT.md"
LICENSE, THIRD_PARTY_NOTICES.md, LICENSES/, README.md, README.ar.md, CONTRIBUTING.md, SECURITY.md, CODE_OF_CONDUCT.md
```

---

## 4. UI conventions (Vue + Tailwind 4 + i18n)

- **Arabic first:** `<html dir>` and `lang` are set from the language setting, and every string is in
  both `ar.json` and `en.json`. Check that the two key sets match in CI or before release.
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
- **State:** a reactive `app` store refreshed by polling `/api/state` every 2–3 s is plenty for a local app.
- **Plain-language features:** show each rule or setting as a sentence ("When … and … then …"), offer
  wizards with templates and a "Try it" preview, and have the activity log state the reason for every
  decision.

---

## 5. Build, packaging, release

`build.ps1 [-Version x.y.z] [-Installer] [-Msix] [-Channel github|store] [-SkipUI] [-SkipTests]`

1. `npm ci && npm run build` writes to `internal/ui/dist`. A Vite plugin re-creates `.gitkeep`.
2. Fetch bundled helpers from pinned versions, verifying SHA-256 against the upstream `checksums.txt`.
3. Run `go vet ./...` and `go test ./...`.
4. `go-winres make --product-version --file-version`, which creates `rsrc_windows_amd64.syso` (gitignored).
5. `go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=… -X main.channel=… -X main.updateRepo=owner/repo"`.
6. Stage `dist/app/` (exe, helpers, LICENSE, notices) and make the portable `.zip`.
7. Build the Inno `Setup.exe`:
   - `PrivilegesRequired=lowest`, `{userpf}` install directory, Arabic and English.
   - `AppUserModelID` on the shortcut, so toasts work.
   - Kill the running app before installing and uninstalling.
   - Remove the `Run` value on uninstall, and ask whether to delete the data folder.
8. Build the MSIX:
   - The `AppxManifest.xml` template has `runFullTrust` and a `startupTask`.
   - The identity comes from `build/msix/store.json` (committed; not secret), which
     `store.local.json` and `MSIX_*` environment variables can override.
   - Store versions end in `.0`.
9. Write `checksums.txt`.

**Workflows** (all checked with `actionlint`):

| File | Trigger | Notes |
|---|---|---|
| `ci.yml` | push, PR, manual | lint, test, full build; the artifact is uploaded **only on manual runs** (saves Actions storage) |
| `release.yml` | tag `vX.Y.Z` | builds, optional SignPath signing, `gh release create` (build the argument list as an array; a `-` in the tag means pre-release) |
| `store.yml` | after a stable release, or manual | builds the MSIX, `msstore reconfigure` + `msstore publish <msix> -id <StoreID>`; gated by the variable `STORE_ENABLED` |
| `pages.yml` | changes under `docs/` | checks Pages is enabled first (skips with a notice), then Jekyll build and deploy |

**In-app updates:** the GitHub build checks the GitHub Releases API (`updateRepo` from ldflags) and only
shows a banner. Bundled helpers can update themselves within the same major version, with checksum
verification and automatic rollback. All of this is disabled in the Store build.

---

## 6. App icon: generated from code (`tools/genicons`)

The app has no hand-drawn icon files. `go run ./tools/genicons` draws everything from one Go function, so
icons are reproducible, easy to restyle, and the tray status variants always match.

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
| `assets/icons/tray-{green,yellow,red,gray}.ico` | 16, 20, 24, 32, 48 | tray: connected, reconnecting or paused, needs action, starting |
| `assets/msix/Square44x44Logo*.png` | 44 + targetsize 24, 48, 256 unplated | MSIX taskbar and Start icons |
| `assets/msix/Square150x150Logo.png`, `Wide310x150Logo.png`, `SplashScreen.png` (620×300) | tiles | MSIX |
| `assets/msix/StoreLogo.png` | 50 | MSIX |
| `assets/store/app-icon-300.png`, `box-art-1080.png`, `poster-720x1080.png` | 300, 1080, 720×1080 | Partner Center listing |

**To reuse:** change the colours and the `inArrow`/bubble shape functions, run the generator, and commit
the outputs. Tray icons are embedded with `//go:embed` in `assets/assets.go`.

**Avoid third-party trademarks** in the icon and the Store name. A generic chat bubble with an arrow passed
Store certification; the word "WhatsApp" in a name is a risk.

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
  - Run with `--data-dir` (isolated; never touches autostart) and `WAM_UI_TOKEN=<hex>` to call the API
    from scripts.
  - Decrypt DPAPI secrets in PowerShell with `ProtectedData.Unprotect` (same entropy) to sign test
    webhooks.
- **UI screenshots:** headless Edge (`--headless=new --force-device-scale-factor=1.5
  --window-size=1280,800 --screenshot=…`), with a separate `--user-data-dir` per shot. Use a **demo
  instance with fictional data**; never screenshot real user data.
- **Installer:** silent install and uninstall (`/VERYSILENT /SUPPRESSMSGBOXES /NORESTART`), then check files,
  shortcut and uninstall entry.
- **MSIX:**
  1. Test-sign with a temporary self-signed certificate (subject equal to the manifest Publisher) trusted
     in `LocalMachine\TrustedPeople`, then `Add-AppxPackage`.
  2. Launch through the app execution alias with `--data-dir`.
  3. Run WACK with the static test IDs.
  4. Remove the package and the certificate.

---

## 8. Publishing checklist (what actually worked)

1. **GitHub:** create an empty repo, push, and turn on Pages with Source = **GitHub Actions**. Workflow
   files can only be pushed with a credential that has the `workflow` scope.
2. **First release:** `git tag v0.1.0 && git push origin v0.1.0`. GitHub Release files don't count
   against Actions storage.
3. **SignPath Foundation:** apply after the first public release, then add `SIGNPATH_*` variables and the
   secret. Until then, tell users about SmartScreen's "More info → Run anyway".
4. **Microsoft Store:**
   - Register a free individual account, or a company account to show a company name.
   - Create the product as **"MSIX or PWA app"**, not "EXE or MSI". The EXE/MSI type requires your own
     trusted code signature and a download URL.
   - Copy Product identity into `build/msix/store.json`.
   - Import the listing as CSV: export the template, fill it, and import the **folder** with the images.
   - Age rating: IARC questionnaire.
   - Explain why the package needs `runFullTrust`.
   - The first submission is by hand. Then create an Entra app (Manager role) and add
     `STORE_TENANT_ID`, `STORE_SELLER_ID`, `STORE_CLIENT_ID` and `STORE_CLIENT_SECRET` to the `store`
     environment, and set `STORE_ENABLED=true`.
5. **Privacy policy URL:** `https://<owner>.github.io/<repo>/privacy.html` (from `docs/privacy.md`).

---

## 9. Gotchas we hit, and the fixes

| Symptom | Cause | Fix |
|---|---|---|
| `go ./...` builds a Go file inside `node_modules` | an npm package ships Go code | add an empty `web/go.mod` to cut `web/` out of the main module |
| gofmt fails in CI on Windows | CRLF line endings | `.gitattributes`: `* text=auto eol=lf` (with `*.ps1`/`*.iss` as CRLF) |
| UI says "session expired" in tests | test token wasn't hex | always use hex tokens |
| `@apply btn` fails | Tailwind 4 | declare with `@utility` |
| i18n message breaks on `name@example.com` | linked-message syntax | `{'@'}` |
| A dev copy can't start while the installed app runs | shared single-instance mutex | scope the mutex by data folder |
| A dev run registered itself for autostart | first-run autostart | skip autostart when `--data-dir` is given |
| Two webhook events for one message, the real one dropped | helper sends an empty "carrier" event first with the same ID | ignore events without content before inserting |
| Links glued to text not detected | `\b` before `https://` | match scheme links without a boundary; generic TLDs for bare domains, excluding file extensions |
| Email clients merge forwarded emails | identical subjects | optional running number in the subject (`[App #42]`) |
| Pages workflow fails "Not Found" | Pages not enabled | turn it on in Settings; the workflow checks first and skips with a notice |
| Release step failed on stable tags | empty `--prerelease` argument passed to `gh` | build the argument array conditionally |
| GitHub rejects the push of workflow files | the OAuth token lacks `workflow` scope | push with a credential that has it (`gh auth refresh -s workflow`) |
| Store product had no "Product identity" | created as EXE/MSI | create a new **MSIX** product |
| Actions storage quota hit | artifacts on every CI run | upload only on manual runs; short retention |
| Headless Edge screenshots blank or missing | shared profile between runs | a separate `--user-data-dir` for each screenshot |
| Phone-width screenshots look clipped | headless Edge's minimum window is about 500 px | judge mobile layout from CSS (`minmax(0,1fr)`), not those shots |

---

## 10. New-project checklist

- [ ] Copy the layout (section 3), `build.ps1`, `build/`, `.github/`, `tools/genicons`, `tools/gennotices`
- [ ] Rename: `AppName` (winx), module path, mutex, data folder, `AppUserModelID`, Inno `AppId` (new GUID),
      MSIX identity placeholders, window title, tray text, README/website names
- [ ] Restyle the icon in `tools/genicons` and run it
- [ ] Write `PROJECT_CONTEXT.md` (decisions + invariants) and `CLAUDE.md` (`@PROJECT_CONTEXT.md`)
- [ ] `ar.json` / `en.json` with the same keys; Arabic default if the audience is Arabic
- [ ] Data folder, DPAPI secrets, single instance, autostart, tray, WebView2 fallback
- [ ] SQLite migrations + daily backup + integrity check + cleanup setting
- [ ] Tests: pure logic, storage, end-to-end with fakes
- [ ] `docs/privacy.md`, user guides, `docs/index.html` website, screenshots from a demo instance
- [ ] CI green → tag `v0.1.0` → apply for SignPath → Store (MSIX) → automate Store submissions
