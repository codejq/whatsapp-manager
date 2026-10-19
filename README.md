<div align="center">
  <img src="assets/icons/app-256.png" width="96" alt="">
  <h1>WhatsApp Manager</h1>
  <p><b>Forward the WhatsApp messages that matter — automatically — to the right people, by WhatsApp or email.</b></p>
  <p><a href="README.ar.md">العربية</a> · <a href="docs/user-guide.en.md">User guide</a> · <a href="../../releases/latest">Download</a></p>
</div>

WhatsApp Manager is a free, open-source Windows app for people who are not technical. Link your WhatsApp
by scanning a QR code, create simple rules, and the app runs quietly in the system tray:

- **When** a message arrives in *the "Sales Leads" group* **and** it *contains a link* → **email it** to `leads@company.com`
- **When** *Ahmed* writes → **forward it on WhatsApp** to `+966 5x xxx xxxx`
- **When** any message contains *"urgent"* or *"عاجل"* → **send it to the "Managers" group**

## Features

- One-click installer, no admin rights, nothing to configure — WhatsApp connectivity is built in.
- Arabic interface by default (right-to-left), English available.
- Rule wizard: watch any chat, all groups, private chats, or specific chats/people; conditions for links,
  keywords (Arabic spelling variants and diacritics are matched automatically), excluded words and message
  types; send to WhatsApp contacts, phone numbers, groups, or email addresses.
- Email through **your own SMTP account** (Gmail, Outlook, Microsoft 365, Yahoo, Zoho, iCloud or any server),
  with auto-detect and a test button.
- Never sends the same message twice to the same destination, never loops, and never floods you with old
  messages after the PC was off.
- Starts with Windows, lives in the tray, reconnects by itself, and recovers from crashes.
- Activity log showing every message, which rule matched and where it went — with "send again".
- Everything stays on your PC. No accounts, no cloud, no telemetry.

## Install

Download `WhatsAppManager-Setup-x.y.z.exe` from the [latest release](../../releases/latest) and run it.
A portable `.zip` is also available.

> Until the app is code-signed, Windows SmartScreen may say "Windows protected your PC". Click
> **More info → Run anyway**. Release files come with SHA-256 checksums.

Requirements: Windows 10 (1809+) or 11, 64-bit, with Microsoft Edge WebView2 (preinstalled on current Windows).

> **Tip:** use a dedicated WhatsApp number. WhatsApp may restrict numbers that send many automated messages.
> The app sends at most 20 WhatsApp messages per minute by default.

## How it works

```
WhatsAppManager.exe (Go)                         gowa\whatsapp.exe (GoWA)
 ├─ tray icon + WebView2 window (Vue UI)          ├─ WhatsApp multi-device client (whatsmeow)
 ├─ local API on 127.0.0.1 (token protected)  ◄── ├─ signed webhook for each incoming message
 ├─ rule engine: inbox → outbox (SQLite)      ──► └─ REST API: forward / send (127.0.0.1, basic auth)
 └─ dispatcher: WhatsApp + SMTP, retries, rate limits
```

WhatsApp connectivity is provided by the unmodified, pinned release of
[GoWA (go-whatsapp-web-multidevice)](https://github.com/aldinokemal/go-whatsapp-web-multidevice), run and
supervised as a child process. See [docs/PLAN.md](docs/PLAN.md) for the full design and
[PROJECT_CONTEXT.md](PROJECT_CONTEXT.md) for the invariants.

## Build from source

Prerequisites: Go (version in `go.mod`), Node.js 22+, PowerShell, and [Inno Setup 6](https://jrsoftware.org/isinfo.php)
for the installer.

```powershell
./build.ps1                          # web UI + tests + exe + portable zip  -> dist/
./build.ps1 -Version 1.0.0 -Installer
```

Development: `go run ./cmd/whatsapp-manager --data-dir .\devdata` runs the app with a separate data folder
(it never registers autostart in this mode). See [CONTRIBUTING.md](CONTRIBUTING.md) and
[docs/RELEASING.md](docs/RELEASING.md).

## Powered by

**Quantum Billing LLC** — [quantum-billing.com](https://quantum-billing.com/) · +1 361-405-5427 ·
[info@Quantum-Billing.com](mailto:info@Quantum-Billing.com)

## License

MIT — see [LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
WhatsApp Manager is not affiliated with, endorsed by, or connected to WhatsApp or Meta.
