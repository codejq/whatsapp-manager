# Project Context — WhatsApp Manager

This file records long-lived facts for any agent or developer working in this repo. Update it when a fact
changes, and keep it short. The full plan is in [docs/PLAN.md](docs/PLAN.md).

## What this is

A Windows desktop app for non-technical users. It connects to their WhatsApp account and **forwards incoming
messages that match user-defined rules** to WhatsApp contacts or groups, or to email.

## Fixed decisions

- **Open source**: public GitHub repo, **MIT licence**. No secrets in the repo. Releases are made by pushing a
  tag `vX.Y.Z` (GitHub Actions on `windows-latest`).
- **Target**: Windows 10/11 x64 only. Per-user install, no admin rights.
- **Distribution**: Phase 1 is GitHub Releases (Inno `Setup.exe` and portable zip; signed through SignPath
  Foundation once approved, unsigned before that). Phase 2 is the **Microsoft Store** as MSIX (signed by
  Microsoft), submitted automatically from CI with `msstore publish`. One codebase serves both, and the app
  detects at runtime whether it is packaged.
- **Email**: sent only through the **user's own SMTP settings** (host, port, STARTTLS/SSL/None, auth, username,
  password, from, reply-to, skip-verify). Presets only pre-fill the form. "Send test email" must pass before
  email rules can be enabled. v1 has one SMTP account. Library: `github.com/wneessen/go-mail`.
- **WhatsApp layer**: the official release binary of [GoWA](https://github.com/aldinokemal/go-whatsapp-web-multidevice)
  (MIT), run as a supervised child process. We **do not fork or import** GoWA.
  - Pinned version: **v9.6.0**. The asset is `whatsapp_9.6.0_windows_amd64.zip`. Verify it against the release's `checksums.txt`.
  - Run with `whatsapp.exe rest`. Working directory = `%LOCALAPPDATA%\WhatsAppManager\gowa` (GoWA resolves
    `storages/` and `statics/` relative to its cwd).
  - Always bind `--host=127.0.0.1`, set `--basic-auth`, `--webhook-secret`, `--webhook-events=message`,
    `--ui-enabled=false` (otherwise it downloads its dashboard from GitHub at start) and `--mcp-enabled=false`.
- **Manager**: Go, `CGO_ENABLED=0`, single exe. SQLite via `modernc.org/sqlite` (WAL). Tray: `fyne.io/systray`.
  Window: WebView2 via `github.com/jchv/go-webview2`, with the default browser as fallback.
- **UI**: Vue 3 + Vite + Tailwind, embedded with `go:embed`. **Arabic is the default language (RTL)** and
  English is available. Use logical CSS (`ms-`/`me-`/`start-`), never `ml-`/`mr-`/`left-`.
- **Data dir**: `%LOCALAPPDATA%\WhatsAppManager\`. Secrets are encrypted with Windows DPAPI.
- **Autostart**: unpackaged builds use `HKCU\...\Run` with `--minimized`. The MSIX build uses the
  `windows.startupTask` extension. Closing the window hides it to the tray.
- **Writable data goes only to `%LOCALAPPDATA%\WhatsAppManager`**, never next to the exe (an MSIX install
  directory is read-only).

## GoWA facts (verified from source, v9.6.0)

- Webhook body: `{event, device_id, session_id?, payload}`. The payload includes `id, chat_id, from, from_lid,
  sender_display_name, from_name, timestamp (RFC3339), is_from_me, body`, plus media objects
  (`image|video|document: {path|url, caption}`; `audio|sticker`: path string).
- Signature: header `X-Hub-Signature-256: sha256=<hex HMAC-SHA256(rawBody, secret)>`.
- **GoWA retries a failed webhook up to 5 times** (1/2/4/8 s backoff), so duplicates are expected.
- Group JIDs end in `@g.us`. Users use `@s.whatsapp.net` or `@lid` (LID migration: match both `from` and `from_lid`).
- Device-scoped REST calls accept the `X-Device-Id` header. With a single device, it is the default.
- `GET /app/login` returns `results.qr_link` (PNG URL) and `qr_duration`. `POST /message/{id}/forward {phone}`
  forwards a stored message (text and media).
- `GET /user/my/groups` returns at most 500 groups (a WhatsApp limit).
- Docs: `docs/openapi.yaml` and `docs/webhook-payload.md` in the GoWA repo.

## Invariants (do not break)

1. **No duplicate deliveries**: `inbox UNIQUE(device_id, message_id, event)` and
   `outbox UNIQUE(device_id, message_id, destination_key)`.
2. **At-most-once on crash**: outbox rows left in `sending` at startup become `uncertain` and are never
   auto-resent.
3. **No loops**: ignore `is_from_me` by default, and ignore the IDs of messages we sent ourselves (`sent_ids`).
4. **No backlog floods**: skip messages older than the rule's `created_at` or older than the catch-up window
   (default 12 h).
5. The webhook handler only stores the event and returns 200. All other work is asynchronous.
6. Everything listens on loopback only. The UI API needs a per-launch token and checks `Host` and `Origin`.
7. Every user-facing string goes through i18n (`ar.json`, `en.json`).
8. The SMTP password and other secrets are stored only DPAPI-encrypted. They are never logged, exported or
   returned by the UI API.
9. SMTP auth failure pauses all email dispatch, and queued emails do not use up retries while it is paused.
   Retries: network, timeout and 4xx are transient; auth and 5xx are permanent.

## Repo map & dev facts

- `cmd/whatsapp-manager`: entry point (tray via `fyne.io/systray`, WebView2 window, single instance).
- `internal/app`: wiring, settings, the local API (`api.go`) and maintenance. `internal/gowa`: client and
  supervisor. `internal/rules`: pure matcher. `internal/engine`: inbox → outbox. `internal/dispatch`:
  senders. `internal/store`: SQLite and migrations. `internal/mail`: SMTP. `internal/webhook`: receiver.
  `internal/winx`: Windows APIs. `internal/ui`: embedded `web/` build (`internal/ui/dist`).
- `web/`: Vue 3 + Vite + Tailwind 4 + vue-i18n 11 + vue-router 5. `web/go.mod` exists only to keep
  `go ./...` out of `node_modules`.
- The Go module path is `whatsapp-manager`, not a GitHub path. The GitHub owner/repo is passed at build
  time (`-UpdateRepo`, `main.updateRepo`).
- GoWA uses the fixed device slot `main` (header `X-Device-Id: main`), created with `POST /devices`.
  `is_logged_in` is false while the socket is down, so "paired" = `is_logged_in || jid != ""`.
- GoWA stores the message before it calls the webhook, so `POST /message/{id}/forward` works for webhook
  messages. A forward that fails permanently falls back to a copy.
- Media in the webhook is either a string path or `{path, caption}` / `{url}`. Paths are relative to GoWA's
  working directory (`<data>\gowa\statics\media`).
- Dev run: `go run ./cmd/whatsapp-manager --data-dir .\devdata`. A custom data dir never touches autostart.
  `WAM_UI_TOKEN` (hex, at least 16 characters) fixes the UI token for automated tests. `WAM_GOWA_EXE`
  overrides the GoWA path.
- The UI token must be hex: the SPA reads it with `/[?&]t=([a-f0-9]+)/`.
- vue-i18n: write `@` in messages as `{'@'}`.
- Build: `./build.ps1 [-Version x.y.z] [-Installer] [-Msix] [-Channel github|store]` → `dist/`.

## Conventions

- Never commit secrets, `*.db`, the GoWA binary, `statics/` or `storages/`.
- The plan and milestones are in `docs/PLAN.md`. Record architecture changes there and in this file.
