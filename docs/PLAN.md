# WhatsApp Manager — Implementation Plan

Status: **Draft v2** · Date: 2026-10-19 · Target OS: Windows 10/11 (x64) · Licence: **MIT, open source on GitHub**

Changes in v2: the user configures their own SMTP server for email (§4.1). The project is open source on GitHub
(§13). Distribution goes through GitHub Releases now and the Microsoft Store later, using an automated pipeline.

## 1. Business case

Small businesses and teams receive important information in WhatsApp groups and chats (links, orders,
announcements, documents). Somebody has to watch those chats and pass the important messages to the right
person by hand. That is slow and messages get missed.

**WhatsApp Manager** is a Windows desktop app that connects to the user's WhatsApp account and **automatically
forwards incoming messages that match rules the user defines** to other WhatsApp contacts or groups, or to email.

Example rules (made in the GUI, without writing code):

| # | When a message… | …and | Then |
|---|---|---|---|
| 1 | arrives in group "Sales Leads" | contains a link | email it to `leads@company.com` |
| 2 | comes from contact "Ahmed" | — | forward it on WhatsApp to `+9665xxxxxxx` |
| 3 | arrives in any chat | contains the keyword "عاجل" or "urgent" | forward it to group "Managers" |
| 4 | arrives in group "Suppliers" from member "Ali" | is a document or image | forward it to contact "Accounts" and email `ap@company.com` |

### Users and success criteria

- Users are **novices**: no command line, no configuration files, no ports, no JIDs.
- Install by downloading **one setup file** and clicking Next. No admin rights needed.
- Connect WhatsApp by scanning a QR code (or entering a pairing code) inside the app.
- Arabic UI by default (right-to-left), English available.
- Runs in the system tray, starts with Windows, and keeps running when the window is closed.
- **Never forwards the same message twice** to the same destination, and never forwards in loops.
- Recovers on its own from crashes, network loss, sleep/resume and WhatsApp disconnects.

### Out of scope for v1

- More than one WhatsApp account at once. The data model includes `device_id` so this can be added later.
- macOS and Linux.
- Rules that send automatic replies (only forwarding).
- Cloud sync and remote management.

## 2. Architecture

```
┌──────────────────────────── WhatsAppManager.exe (Go, single binary) ─────────────────────────────┐
│                                                                                                   │
│  Tray icon (systray) ── opens ──► App window (WebView2)  ──HTTP──►  Local API (127.0.0.1:P1)      │
│                                     Vue 3 SPA, embedded                     │                     │
│                                                                             ▼                     │
│   Webhook receiver  ◄── HMAC-signed POST ──┐            ┌──── Rule engine ◄── Inbox (SQLite)      │
│   /hook (127.0.0.1:P1)                     │            │          │                              │
│                                            │            │          ▼                              │
│   GoWA supervisor ── starts/watches ──► gowa.exe        │      Outbox (SQLite) ──► Dispatcher      │
│                                         (127.0.0.1:P2)  │                          │       │       │
│                                            ▲            │                 WhatsApp send   SMTP     │
│                                            └── REST (basic auth) ◄─────────────┘                  │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼  WhatsApp servers (via whatsmeow inside GoWA)
```

### 2.1 Why this shape

| Decision | Choice | Reason |
|---|---|---|
| WhatsApp connectivity | Run the **official GoWA Windows release binary** (`whatsapp_<ver>_windows_amd64.zip`) as a supervised child process | We don't maintain a fork. Upgrades are a version bump with a checksum. A GoWA crash can't take down the manager. GoWA uses package globals and CGO SQLite, so it is a poor fit for importing as a library. MIT licence allows bundling. |
| Manager language | **Go** (pure Go, `CGO_ENABLED=0`) | Produces one static `.exe` and needs no runtime. Same ecosystem as GoWA. Good Windows API access (registry, mutex, job objects). |
| GUI | **Vue 3 + Vite + Tailwind**, built to static files embedded with `go:embed`, shown in a **WebView2** window (`github.com/jchv/go-webview2`, no CGO). Falls back to the default browser if WebView2 is missing. | WebView2 is built into Windows 10/11. Web UI gives full RTL/Arabic support and a modern look. Tailwind logical utilities (`ms-*`, `me-*`, `start-*`) make RTL and LTR work from one codebase. |
| Tray | `fyne.io/systray` | Pure Go, works on Windows. |
| Storage | SQLite via `modernc.org/sqlite` (pure Go), WAL mode | No CGO, one file, crash-safe transactions. |
| Packaging | **Phase 1:** Inno Setup `Setup.exe` (per-user, `PrivilegesRequired=lowest`) and a portable `.zip` on **GitHub Releases**. **Phase 2:** an **MSIX** package submitted to the **Microsoft Store**, which signs it for free. | Both packages come from the same build. No UAC prompt. Arabic and English installer languages. See §13. |
| Email | Built-in SMTP client (`github.com/wneessen/go-mail`) using the **user's own SMTP settings** (§4.1). Presets only pre-fill the form. | Works with any provider: Gmail, Outlook/Office 365, Zoho, a hosting company's mail server, or a company Exchange server. |

### 2.2 Runtime layout

```
%LOCALAPPDATA%\Programs\WhatsAppManager\     (Inno install dir, read-only at runtime;
                                              the MSIX build installs under WindowsApps instead)
  WhatsAppManager.exe
  gowa\whatsapp.exe                           (pinned GoWA version, SHA-256 verified at build time)
  LICENSES\                                   (GoWA MIT licence + third-party notices)

%LOCALAPPDATA%\WhatsAppManager\               (data dir, all mutable state)
  manager.db                                  (rules, inbox, outbox, settings, logs index)
  gowa\                                       (GoWA working dir: storages\, statics\)
  logs\manager.log, logs\gowa.log             (rotated, 10 MB × 5)
  backups\                                    (daily manager.db backup, keep 7)
```

GoWA resolves `storages/` and `statics/` relative to its **working directory**, so the supervisor
starts it with `cwd = %LOCALAPPDATA%\WhatsAppManager\gowa`.

## 3. GoWA integration (exact contract)

Pinned version: **v9.6.0** (latest release on 2026-10-19). The pin is in `build/gowa.version` together with
the SHA-256 from the release's `checksums.txt`.

### 3.1 Launch command

```
gowa\whatsapp.exe rest
  --host=127.0.0.1 --port=<P2>
  --basic-auth=wam:<random-32-byte-secret>
  --webhook=http://127.0.0.1:<P1>/hook
  --webhook-secret=<random-32-byte-secret>
  --webhook-events=message
  --ui-enabled=false --ui-auto-update=false
  --mcp-enabled=false
  --os="WhatsApp Manager"
  --auto-download-media=true
  --presence-on-connect=unavailable
```

- Secrets are generated on first run and stored in `manager.db`. The ports are picked from free ports and
  saved; if a saved port is taken, a new one is picked.
- GoWA listens **only on loopback**, with basic auth, so other machines and other local users can't reach it.
- `--ui-enabled=false` stops GoWA from downloading its web dashboard from GitHub at startup (we don't use it, and
  it would fail offline).
- The child runs inside a **Windows Job Object** with `KILL_ON_JOB_CLOSE`, so it never outlives the manager.
  It is launched with `CREATE_NO_WINDOW`.

### 3.2 Endpoints used

| Purpose | Endpoint |
|---|---|
| Health and connection state | `GET /app/status`, `GET /health` |
| QR login | `GET /app/login` → `results.qr_link` (PNG; the manager fetches and proxies it), `qr_duration` |
| Pairing-code login | `GET /app/login-with-code?phone=` |
| Logout / reconnect | `GET /app/logout`, `GET /app/reconnect` |
| Group picker | `GET /user/my/groups` (WhatsApp limits this to 500 groups) |
| Contact picker | `GET /user/my/contacts`, `GET /chats` |
| Validate a phone number | `GET /user/check?phone=` |
| Native forward | `POST /message/{id}/forward` `{phone}` (text and media; reuses media refs, re-uploads if stale) |
| Copy-send fallback | `POST /send/message`, `/send/image`, `/send/file`, `/send/video` |

### 3.3 Incoming webhook

- Body: `{event, device_id, session_id?, payload:{id, chat_id, from, from_lid, sender_display_name, from_name,
  timestamp, is_from_me, body, image|video|document|audio|sticker|contact|location…, replied_to_id, quoted_body}}`.
- Verify header `X-Hub-Signature-256: sha256=<hex HMAC-SHA256(body, secret)>` with a constant-time compare. Reject
  the request on a mismatch.
- **GoWA retries a delivery up to 5 times (1+2+4+8 s backoff)** if the request fails or times out, so the same
  event can arrive more than once. The receiver must be idempotent (see §5).
- The receiver only **stores the raw event in `inbox` in one transaction and returns 200**, with a target time
  under 50 ms. All processing is asynchronous, so a slow rule never causes GoWA to retry.
- Group chat JIDs end in `@g.us`. Contacts use `@s.whatsapp.net` or `@lid`. A rule stores both the phone JID and
  the LID when they are known, and matching checks `from`, `from_lid`, `chat_id` and `chat_lid`.

## 4. Rules model

A rule is **WHEN (source) + IF (conditions) → THEN (actions)**. The editor is a 3-step wizard with plain-language
sentences, for example: "When a message arrives in **Sales Leads** and it **contains a link**, then **email it to**
leads@company.com".

```jsonc
Rule {
  id, name, enabled, priority,
  created_at,                           // messages older than this are never processed by this rule
  source: {
    kind: "any" | "any_group" | "any_private" | "chats",
    chats: [{jid, lid?, name}],         // picked from lists, never typed
    senders: [{jid, lid?, name}]        // optional: only these members (inside groups) or contacts
  },
  conditions: {
    match: "all" | "any",
    items: [
      {type: "contains_link"},
      {type: "keywords", words: ["urgent","عاجل"], mode: "any"|"all", whole_word: bool},
      {type: "message_type", in: ["text","image","video","document","audio","location","contact"]},
      {type: "not_contains", words: [...]}
    ]                                   // an empty list means every message from the source matches
  },
  actions: [
    {type: "whatsapp", to: {jid, name}, mode: "forward" | "copy_with_header"},
    {type: "email", to: ["a@b.com"], include_media: true}
  ],
  options: { include_own_messages: false, cooldown_seconds: 0 }
}
```

- **Text normalization** for keyword and link matching: Unicode NFKC, case-folding, removing Arabic diacritics
  (tashkeel) and tatweel, and unifying أ/إ/آ→ا, ى→ي and ة→ه. Without this, Arabic keywords miss obvious matches.
  Text is taken from `body`, media `caption`, `quoted_body` (optional) and document filenames.
- **Link detection**: a URL regex (http/https/www., plus bare domains with a common TLD) and WhatsApp
  `extendedTextMessage` previews.
- **Email content**: sent through the SMTP account configured in §4.1. Subject `[WhatsApp] <chat name> — <sender>`. The body includes the sender, chat, time and
  text, as an RTL-aware HTML and plain-text multipart message. Media from `statics/media` is attached up to
  20 MB (the rest is linked as "too large").
- **WhatsApp `copy_with_header` mode** sends "📩 From <sender> in <chat>:\n<text>" plus the media, for users who
  want to know where the message came from. `forward` mode uses the native forward.
- Built-in **rule templates**: "Links from a group → email", "Everything from a contact → WhatsApp number",
  "Keyword anywhere → group".
- **Test rule** button: the user pastes sample text and picks a chat, and the app shows which rules would fire
  and why, without sending anything.
- A rule that has an email action cannot be enabled until SMTP is configured and has passed a test. The editor
  links to the SMTP settings.

### 4.1 SMTP settings (email delivery)

The app sends email only through an SMTP server that the user configures. There is no built-in mail relay and
no third-party email API.

**Fields** (Settings → Email):

| Field | Notes |
|---|---|
| Provider preset | Gmail · Outlook/Office 365 · Yahoo · Zoho · iCloud · **Custom**. A preset only fills in host, port and security; every field stays editable. |
| SMTP host | for example `smtp.gmail.com` or `mail.mycompany.com` |
| Port | 587 (STARTTLS) by default; 465 (SSL/TLS) and 25 are allowed |
| Security | `STARTTLS` (default) · `SSL/TLS` · `None`. Choosing `None` shows a warning, and auth is refused without encryption unless "Allow insecure" is ticked. |
| Authentication | `Login/Plain` (default) · `CRAM-MD5` · `None` (for internal relays) |
| Username | defaults to the From address |
| Password / app password | stored encrypted with DPAPI and never shown again (masked, with a "Change" button). Contextual help explains app passwords for Gmail and Outlook (ar/en). |
| From address and display name | for example `WhatsApp Manager <alerts@mycompany.com>` |
| Reply-To (optional) | |
| Skip TLS certificate check | off by default; for self-signed internal servers, shown under "Advanced" with a warning |
| Timeout | 30 s by default (Advanced) |

**Behaviour:**

- **"Send test email"** sends a test message to an address the user chooses. If it fails, the app shows a
  friendly, translated reason with the raw server reply under "Details". The reasons it recognises are: cannot
  reach host or port, TLS handshake failed, certificate invalid, wrong username or password, sender address
  rejected, and recipient rejected.
- **"Auto-detect"**: when only an email address and password are entered, the app tries the preset for the
  domain, then `smtp.<domain>` on ports 587 and 465. It saves the first combination that works.
- Settings are validated before saving: the host must not be empty, the port must be 1–65535, and the From
  address must be a valid email.
- Changing the SMTP settings **does not cancel queued emails**. Queued emails are sent with the settings in
  effect when they are sent. If the settings were never tested, or the last test failed, email jobs wait in the
  outbox with the status "Waiting for email setup" and do not use up retry attempts.
- **Error classification** for retries: network, timeout and 4xx SMTP replies are transient, so the email is
  retried with backoff (§5). Auth failures and 5xx replies are permanent. On an auth failure, all email
  dispatch is paused, a toast says "Email login failed — check settings", and the queued emails resume
  automatically after a successful test.
- One SMTP connection is reused for a burst of emails and closed after 60 s idle. A limit of 30 emails per
  minute avoids provider throttling.
- The import/export of settings excludes the password.
- **v1 has one SMTP account.** The table is designed so per-rule accounts can be added later.

## 5. Correctness: no duplicates, no loops, no surprises

| Risk | Guard |
|---|---|
| GoWA retries the webhook | `inbox` has `UNIQUE(device_id, message_id, event)` and duplicate inserts are ignored. |
| Two rules send the same message to the same destination | `outbox` has `UNIQUE(device_id, message_id, destination_key)`, where `destination_key` = `wa:<jid>` or `mail:<lowercased address>`. The first rule wins and the others are logged as "already sent by rule X". |
| Reprocessing after a crash or restart | Inbox rows move `new → processed` inside the same transaction that writes the outbox rows. Processing is restartable and idempotent. |
| Crash while a message is being sent | Outbox state goes `pending → sending → sent/failed`. On startup, rows still in `sending` become **`uncertain`** and are **not auto-resent** (at-most-once). The Activity screen shows them with a "Send again" button. |
| Transient send failure | Retry with exponential backoff (30 s, 2 m, 10 m, 30 m, 2 h; maximum 6 attempts), then `failed` with a visible reason. Errors are classified: auth/not-on-WhatsApp/invalid address → permanent, so no retry. |
| Forward loops (A→B→A, or forwarding into a monitored group) | 1) Messages with `is_from_me=true` are ignored unless the rule opts in. 2) Every message ID we send is recorded in `sent_ids`, and incoming events with those IDs are ignored. 3) The rule editor warns when a destination is also a source of any rule. |
| Old messages arriving after reconnect (offline backlog) | A message is ignored if its `timestamp` is older than the rule's `created_at` or older than **Max catch-up age** (default 12 h, configurable). |
| Edited, revoked or reaction events | Only `event=message` is subscribed. Reactions, polls-votes and protocol messages are ignored. |
| Spam bursts / WhatsApp ban risk | A global rate limiter on outbound WhatsApp sends (default 20/min, with jitter). The per-rule `cooldown_seconds` is optional. A queue badge appears when the queue is backed up. |
| Clock skew | Ordering and dedup use message IDs, not time. Time is used only for the catch-up window. |

## 6. Resilience

- **Single instance**: a named mutex `Global\WhatsAppManager-<user SID>`. Starting the app again only brings
  the window of the running instance to the front (over a named pipe).
- **Supervisor**: if GoWA exits, it is restarted with backoff (1 s → 60 s cap), and the backoff resets after
  5 minutes of healthy uptime. `/app/status` is polled every 15 s. If the device is logged in but disconnected
  for over 60 s, `/app/reconnect` is called. A **logged-out** state shows a tray notification and a "Reconnect
  WhatsApp" banner.
- **Sleep and resume**: the app listens for `WM_POWERBROADCAST`/`PBT_APMRESUMEAUTOMATIC` and triggers a health
  check plus reconnect.
- **Panic safety**: every goroutine is wrapped with recover and logging. The rule engine skips a bad rule instead
  of crashing.
- **Database**: WAL mode, `busy_timeout=5000`, a daily online backup, and `PRAGMA integrity_check` on start. If
  the database is corrupt, the app restores the latest backup and tells the user.
- **Cleanup period** (Settings → General, default **5 days**, 1–365): messages, activity history and GoWA
  `statics/media` older than the period are deleted hourly, and right away when the period is changed.
  Deliveries still waiting to be sent are never deleted. The sent-message IDs that guard against loops are
  kept for at least the catch-up window plus 48 h.
- **Email subject counter** (Settings → Email, on by default): each email subject gets a running number
  (`[WhatsApp #42] …`) so Gmail and Outlook do not group separate forwards into one conversation. The
  counter is persisted in `settings` (`email_subject_counter`) and incremented atomically.
- **Logs**: rotating files plus an in-app "Activity" view that shows each message, which rule matched, and the
  delivery status. A "Copy diagnostics" button zips the logs and settings (without secrets) for support.
- **Ports**: if a port is in use, the app picks another one and restarts GoWA with the new webhook URL.
- **Graceful shutdown**: "Quit" in the tray drains the in-flight sends (5 s), stops GoWA cleanly, and then exits.

## 7. Windows integration

- **Start with Windows**: on by default, with a toggle in Settings. No admin rights needed. At runtime the app
  detects how it was installed (`GetCurrentPackageFullName`):
  - Inno/portable: `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\WhatsAppManager = "<exe>" --minimized`.
  - MSIX/Store: the `windows.startupTask` manifest extension, controlled through the WinRT `StartupTask` API.
    The user can also disable it in Task Manager, and the app reflects that state.
- **Close button hides the window to the tray**. A one-time tooltip says "Still running in the tray". Quit is
  in the tray menu.
- **Tray menu**: Open · Status (Connected/Disconnected) · Pause all rules · Language · Quit. The icon shows
  green, yellow or red for the connection state.
- **Toast notifications** (`github.com/go-toast/toast` or the WinRT API) for disconnect, logout, and repeated
  failures.
- **Code signing**: see §13.3. The Store build is signed by Microsoft. The GitHub build is signed through the
  **SignPath Foundation** (free for open-source projects) once the project qualifies. Until then, it is
  released unsigned, and the README explains the SmartScreen "More info → Run anyway" step.

## 8. GUI (Arabic default, English)

Screens:

1. **First-run wizard**: language → connect WhatsApp (QR shown large, auto-refreshes, with a "Use phone number
   instead" pairing code option) → optional email setup → create the first rule from a template.
2. **Dashboard**: connection card, today's counts (received / matched / forwarded / failed), and recent activity.
3. **Rules**: list with on/off toggles, plus duplicate, delete and test actions. The wizard editor (§4) uses
   searchable pickers for groups and contacts with avatars and names, and the user can also type a phone
   number with country code (validated with `/user/check`).
4. **Activity**: filterable log of messages, matched rules and delivery status, with "Send again" for failed or
   uncertain items.
5. **Settings**: WhatsApp account (logout or switch), **Email/SMTP** (all fields in §4.1, "Send test email",
   "Auto-detect", and a status badge: Not configured / Working / Failing), start with Windows, catch-up age, rate limit, language, theme, backup/restore rules (JSON export and
   import), and about/licences.

i18n uses `vue-i18n` with `ar.json` (default) and `en.json`. `<html dir>` switches between `rtl` and `ltr`. Fonts
(**IBM Plex Sans Arabic** / Noto Sans Arabic and Inter) are embedded, so the UI works offline. Numbers and
dates are formatted for the chosen locale. All error messages shown to users come from the i18n files; raw API
errors appear only in the details view.

Secrets (the SMTP password and GoWA credentials) are encrypted at rest with **Windows DPAPI**
(`CryptProtectData`, user scope).

## 9. Repository layout

```
/cmd/whatsapp-manager/main.go       entry point: single-instance, tray, window, wiring
/internal/app/                      lifecycle, shutdown, settings
/internal/gowa/                     supervisor (process, job object, ports), typed REST client
/internal/webhook/                  HMAC-verified receiver → inbox
/internal/rules/                    model, normalization, matcher (pure, heavily unit-tested)
/internal/engine/                   inbox → match → outbox (transactional)
/internal/dispatch/                 outbox workers: whatsapp, email; retry and rate limit
/internal/store/                    SQLite, migrations (embedded SQL), backup
/internal/winx/                     autostart, DPAPI, mutex, power events, toasts
/internal/api/                      local HTTP API for the UI (loopback + per-launch token)
/web/                               Vue 3 + Vite + Tailwind SPA (built into /internal/api/dist)
/internal/mail/                     SMTP client, presets, auto-detect, error classification
/build/                             gowa.version, fetch-gowa script, versioninfo, icons
/build/inno/                        Inno Setup .iss (ar/en)
/build/msix/                        AppxManifest.xml template, Store assets (logos, screenshots)
/.github/workflows/                 ci.yml, release.yml, store.yml
/.github/ISSUE_TEMPLATE/            bug report (asks for "Copy diagnostics" zip), feature request
/docs/                              PLAN.md, decisions, user guide (ar/en), privacy policy
LICENSE  README.md  README.ar.md  CONTRIBUTING.md  SECURITY.md  CODE_OF_CONDUCT.md  THIRD_PARTY_NOTICES.md
```

The local UI API listens on loopback and requires a random per-launch token passed to the WebView. This stops
other local web pages from calling it (CSRF/DNS-rebinding guard: check `Host` and `Origin`).

## 10. Milestones

| # | Milestone | Done when |
|---|---|---|
| M0 | Scaffold | Repo on GitHub with the MIT LICENSE, README (ar/en) and CONTRIBUTING; Go module; Vue app; `build.ps1`; `ci.yml` on `windows-latest` (lint, test, build) produces an unsigned exe as a workflow artifact |
| M1 | GoWA supervisor | Downloads and verifies GoWA, starts and stops it, survives kill -9 of the child, shows status |
| M2 | Login | QR and pairing code login in the UI; logout; reconnect after reboot |
| M3 | Webhook + inbox | Signed events stored idempotently; duplicate-delivery test passes |
| M4 | Rule engine | Matcher with normalization; table-driven tests (Arabic and English); test-rule UI |
| M5 | Dispatch | WhatsApp forward/copy; SMTP settings with test and auto-detect (§4.1); outbox state machine; retry; rate limits; loop guards |
| M6 | GUI complete | All screens, AR/EN, RTL, first-run wizard, templates |
| M7 | Windows polish | Tray, autostart, toasts, single instance, sleep/resume, DPAPI |
| M8 | GitHub release | `release.yml` on tag `v*` publishes the Inno `Setup.exe`, portable zip and `checksums.txt` to GitHub Releases; SignPath signing once approved |
| M9 | Hardening | Soak test 72 h; chaos tests (kill GoWA, network off, DB locked, port taken, SMTP down or wrong password); user guide |
| M10 | Microsoft Store | MSIX build in CI; Partner Center app reserved; privacy policy published; first manual submission; then `store.yml` automates later submissions |

## 11. Testing strategy

- **Unit**: matcher and normalization (golden tables), dedup keys, the outbox state machine, and retry
  classification.
- **Integration**: a fake GoWA HTTP server that sends signed webhooks, including duplicates and out-of-order
  delivery, and records sends. These tests assert exactly-once outbox creation and the loop guards.
- **Process tests**: the supervisor restarts a crashing fake child; the job object kills the child when the
  parent dies.
- **Manual E2E**: a real test WhatsApp number with two groups, checking each template rule, plus a reboot test
  and a sleep/resume test.

## 12. Risks

| Risk | Mitigation |
|---|---|
| WhatsApp bans automated accounts | Rate limiting, presence `unavailable`, no bulk sending, and a warning in the docs to use a dedicated number. |
| GoWA breaking changes | Pinned version; the integration contract is kept in one package (`internal/gowa`); contract tests; deliberate upgrades. |
| SmartScreen/AV warnings | Store build (signed by Microsoft); SignPath for GitHub builds; submit false positives to Microsoft; avoid UPX packing. |
| SMTP providers require app passwords or OAuth (Gmail and Microsoft 365 may disable basic auth) | Any SMTP server works; in-app help for app passwords (ar/en); clear auth-failure messages. OAuth2 (XOAUTH2) for Gmail and Microsoft is a later option. |
| Store policy on WhatsApp automation | The listing describes the app as a personal message-routing tool that uses the user's own account; no WhatsApp branding in the name or icon; privacy policy states that all data stays on the PC. If the Store rejects the app, GitHub Releases remains the channel. |
| WebView2 missing (old Windows 10) | The installer checks for it and offers the Evergreen bootstrapper; the app falls back to the browser. |

## 13. Open source, release and distribution

### 13.1 Repository

- Public GitHub repository, **MIT licence** (the same as GoWA, so bundling is simple). `THIRD_PARTY_NOTICES.md`
  lists GoWA and every bundled Go and npm dependency, generated in CI with `go-licenses` and `license-checker`.
- The README is in Arabic and English, with screenshots and a "Download" button that links to the latest
  release (later also a Store badge).
- `SECURITY.md` explains private vulnerability reporting through GitHub Security Advisories.
- Contributions go through PRs, and CI must pass. `main` is protected. Releases are made by pushing a SemVer
  tag `vX.Y.Z`.
- **No secrets in the repo.** The GoWA binary is downloaded in CI and checked against the pinned SHA-256 in
  `build/gowa.version`. Signing and Store credentials are stored only as GitHub Actions secrets in a protected
  `release` environment.

### 13.2 CI/CD workflows (GitHub Actions, `windows-latest`)

| Workflow | Trigger | Steps |
|---|---|---|
| `ci.yml` | push, PR | `go vet`, `staticcheck`, `go test -race` (with a CGO-enabled toolchain for `-race` only), `npm ci && npm run lint && npm run build`, build the exe, upload it as an artifact |
| `release.yml` | tag `v*` | Build the UI and exe with the version from the tag (ldflags plus `goversioninfo`); fetch and verify GoWA; build the Inno `Setup.exe` and portable zip; **sign through SignPath** (when enabled); generate `checksums.txt` and an SBOM; create a GitHub Release with notes from the changelog |
| `store.yml` | after `release.yml` succeeds on a non-prerelease tag, or manually | Build the **MSIX** (`makeappx`, with the manifest version taken from the tag); submit it to Partner Center with the **Microsoft Store Developer CLI** (`msstore publish`), authenticating with an Entra ID app registration (tenant ID, client ID and secret, seller ID as secrets) |

Builds are reproducible: Go and Node versions are pinned, `-trimpath`, and `npm ci` uses the lockfile.

### 13.3 Signing and channels

| Channel | Package | Signing | Updates |
|---|---|---|---|
| GitHub Releases (now) | `WhatsAppManager-Setup-x.y.z.exe`, `WhatsAppManager-x.y.z-portable.zip` | **SignPath Foundation** (free for OSS). Requirements: OSI licence, public repo, automated CI build, a released version. Apply after the first public release. Unsigned until then. | An in-app "Update available" check against the GitHub Releases API. The user downloads and installs it; the app never updates itself silently. |
| Microsoft Store (later) | MSIX (`x64`) | Signed by Microsoft on Store ingestion. Free with the individual developer account. | Automatic, through the Store. The in-app update check is disabled in the MSIX build. |

The MSIX package has the `runFullTrust` capability (Win32 app), a `windows.startupTask` extension, and the
bundled `gowa\whatsapp.exe`. All writable data goes to `%LOCALAPPDATA%\WhatsAppManager`, which works in both
packaged and unpackaged builds. The app never writes next to its own exe.

### 13.4 Store readiness checklist (M10)

- Partner Center individual account (free; ID and selfie verification), app name reserved.
- Privacy policy page (GitHub Pages, ar/en): no data leaves the PC except the WhatsApp traffic and the emails
  the user configures; no telemetry.
- Store listing in Arabic and English, screenshots, an age rating questionnaire, and a description that makes
  clear the app is not affiliated with WhatsApp or Meta.
- Run the Windows App Certification Kit (WACK) in CI on the MSIX.

## 13.5 GoWA updates

The GitHub build checks for newer GoWA releases daily. It offers same-major versions in Settings → WhatsApp
and in a banner. Installing verifies the SHA-256 from the release's `checksums.txt`, restarts GoWA on the new
binary (the WhatsApp session is kept, because GoWA's working directory does not change), and rolls back
automatically if it does not start. Major versions are not installed, because they may break the REST or
webhook contract. The feature is off in the Store build.

## 14. Implementation status (2026-10-19)

| Milestone | Status | Notes |
|---|---|---|
| M0 Scaffold | ✅ Done | Repo files (LICENSE, README ar/en, CONTRIBUTING, SECURITY, CoC), `build.ps1`, `ci.yml` |
| M1 GoWA supervisor | ✅ Done | Verified live: GoWA killed → restarted in about 2 s; manager killed → the job object killed GoWA |
| M2 Login | ✅ Done | QR (verified live, real WhatsApp QR) and pairing code; logout; reconnect |
| M3 Webhook + inbox | ✅ Done | Verified live: the same signed webhook ×3 → 1 inbox row; a bad signature → 401 |
| M4 Rule engine | ✅ Done | Table-driven tests in Arabic and English; Try-it endpoint and UI |
| M5 Dispatch | ✅ Done | Verified live against a local SMTP server; integration tests cover dedup, loops, offline, permanent failure |
| M6 GUI | ✅ Done | All screens in ar/en with RTL; checked with headless screenshots |
| M7 Windows polish | ✅ Done | Tray, autostart, toasts, single instance, sleep/resume, DPAPI |
| M8 GitHub release | ✅ Pipeline ready | `release.yml`; the installer was verified locally with a silent install and uninstall. SignPath needs an application after the first release |
| M9 Hardening | 🟡 Partial | Chaos checks done (GoWA kill, manager kill, bad auth/host/signature). **Still to do:** a 72 h soak test and an end-to-end test with a real WhatsApp number (needs a phone) |
| M10 Microsoft Store | 🟡 Pipeline ready | Manifest, assets, `build-msix.ps1` and `store.yml` exist. Needs the Partner Center account and secrets (see docs/RELEASING.md). The MSIX was not built locally because the Windows SDK is not installed here; CI runners have it |

Deviations from the plan, with reasons:

- **Sleep/resume** is detected from a gap between supervisor ticks (more than 2× the interval), not from
  `WM_POWERBROADCAST`. It needs no window message hook and covers hibernate and long stalls too.
- **Second-instance activation** goes through a token-protected loopback endpoint (`/ipc/show`, with the
  port and token in `instance.json`), not a named pipe. It reuses the existing HTTP server.
- **SMTP** opens a new connection for each email instead of reusing one connection with a 60 s idle timeout.
  Volumes are low, and this is simpler and more robust. The rate limit (30/min) still applies.
- **Toasts** use the WinRT toast API through a hidden PowerShell process. The installer sets the
  AppUserModelID on the Start-menu shortcut.
- **MSIX autostart**: the StartupTask cannot pass `--minimized`, so a packaged launch within 5 minutes of
  boot starts in the tray.
- **The WACK run in CI** is not wired yet. Run it manually before the first Store submission.
