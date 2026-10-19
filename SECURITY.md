# Security policy

Please report vulnerabilities privately through **GitHub → Security → Report a vulnerability**
(private security advisories). Do not open public issues for security problems.

How the app protects itself:

- The local API and GoWA listen on `127.0.0.1` only.
- The UI API requires a per-launch token and checks the `Host` and `Origin` headers.
- GoWA uses random basic-auth credentials, and its webhooks are signed with HMAC-SHA256.
- Secrets (the SMTP password, GoWA credentials and the webhook secret) are encrypted with Windows DPAPI for
  the current user.
- The bundled GoWA binary is pinned and its SHA-256 is verified at build time.

We aim to acknowledge reports within 7 days.
