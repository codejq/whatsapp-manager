# Contributing

Thanks for helping! Please read [PROJECT_CONTEXT.md](PROJECT_CONTEXT.md) first. It lists the decisions and
invariants: no duplicate deliveries, at-most-once on crash, loopback only, and i18n for every string.

## Setup

```powershell
winget install GoLang.Go OpenJS.NodeJS.LTS JRSoftware.InnoSetup
npm --prefix web ci
powershell -File build/fetch-gowa.ps1      # pinned GoWA binary -> build/gowa/
```

Run the app against a throwaway data folder. In this mode it never touches autostart:

```powershell
npm --prefix web run build
go run ./cmd/whatsapp-manager --data-dir .\devdata
```

## Checks before a PR

```powershell
gofmt -l cmd internal tools assets   # must print nothing
go vet ./...
go test ./...
npm --prefix web run lint
npm --prefix web run build
```

## Guidelines

- Keep Go packages focused (`internal/<area>`). The rule matcher (`internal/rules`) stays pure.
- Every user-visible string goes into **both** `web/src/locales/ar.json` and `en.json`. Use logical CSS
  (`ms-`/`me-`/`start-`/`end-`), never `ml-`/`mr-`/`left-`/`right-`. Do not put a bare `@` in
  translations: vue-i18n treats it as a link, so write `{'@'}`.
- Backend errors shown to users are i18n keys (`errors.*`), not raw text.
- A schema change goes in a new file in `internal/store/migrations/`. Never edit an applied migration.
- To upgrade GoWA, update `build/gowa.version` (the version and the SHA-256 from the release's
  `checksums.txt`), then check the webhook payload and REST contract in `internal/gowa`.
- Icons are generated with `go run ./tools/genicons` and third-party notices with `go run ./tools/gennotices`.
