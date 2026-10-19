# Releasing

## GitHub release (every version)

1. Make sure `main` is green in CI.
2. Tag and push:
   ```powershell
   git tag v1.2.0
   git push origin v1.2.0
   ```
3. `.github/workflows/release.yml` builds the app, the installer and the portable zip, signs them (once
   SignPath is enabled), and publishes a GitHub Release with `checksums.txt`. A tag containing `-` (for
   example `v1.3.0-beta.1`) is published as a pre-release and is not sent to the Store.

Local equivalent: `./build.ps1 -Version 1.2.0 -Installer -UpdateRepo owner/repo` (output goes to `dist/`).

## Code signing with SignPath Foundation (free for open source)

1. After the first public release, apply at https://signpath.org (requirements: an OSI licence, a public
   repo, an automated build, and at least one release).
2. In SignPath, create the project and its signing policy, and connect the GitHub repo as a trusted build
   system.
3. Add these to the repo. The release workflow skips signing while they are missing.
   - Variables: `SIGNPATH_ORGANIZATION_ID`, `SIGNPATH_PROJECT_SLUG`, `SIGNPATH_POLICY_SLUG`
   - Secret: `SIGNPATH_API_TOKEN`
4. Each signing request needs manual approval in SignPath, as required by their OSS terms.

## Microsoft Store (MSIX)

One-time setup:

1. Register a free individual developer account at https://storedeveloper.microsoft.com. Verification
   uses an ID and a selfie.
2. In Partner Center, reserve the app name. Under **Product identity**, note the *Package/Identity/Name*,
   *Package/Identity/Publisher* and *Publisher display name*.
3. Publish the privacy policy (see [privacy.md](privacy.md), for example through GitHub Pages) and put its
   URL in the Store listing.
4. Create the Store listing in Arabic and English, add screenshots, and complete the age rating. Say clearly
   that the app is not affiliated with WhatsApp or Meta.
5. Make the **first submission manually**: run `./build.ps1 -Version 1.2.0 -Channel store -Msix` with the
   `MSIX_*` environment variables set, then upload `dist/WhatsAppManager-1.2.0.0.msix`. The Store needs the
   `runFullTrust` capability justification: "Desktop app that runs a local WhatsApp client and shows a tray
   icon".
6. For automation, create a Microsoft Entra ID app registration, add it to Partner Center
   (**Account settings → User management → Microsoft Entra applications**, Manager role), and add these
   secrets in a `store` environment:
   `STORE_TENANT_ID`, `STORE_SELLER_ID`, `STORE_CLIENT_ID`, `STORE_CLIENT_SECRET`, `STORE_PRODUCT_ID`,
   `MSIX_IDENTITY_NAME`, `MSIX_PUBLISHER`, `MSIX_PUBLISHER_DISPLAY`.

After that, `.github/workflows/store.yml` runs after every stable release, or manually, and submits the
MSIX with `msstore publish`. The Store signs the package. The Store build:

- has the update check turned off, because the Store updates the app;
- starts with Windows through the MSIX `StartupTask` (users manage it in Task Manager → Startup apps). A
  launch within 5 minutes of boot starts minimized to the tray;
- keeps data in `%LOCALAPPDATA%\WhatsAppManager`, which Windows redirects into the package's private
  storage.

To test an MSIX locally before the Store signs it, sign it with a self-signed certificate whose subject
equals `MSIX_PUBLISHER`, trust that certificate, and install the package:

```powershell
$cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject "CN=WhatsAppManagerDev" -CertStoreLocation Cert:\CurrentUser\My
signtool sign /fd SHA256 /sha1 $cert.Thumbprint dist\WhatsAppManager-1.2.0.0.msix
```

## Upgrading the bundled GoWA

1. Read the GoWA release notes for breaking changes to the webhook payload or the REST API.
2. Update `VERSION`, `ASSET`, `SHA256` (from the release's `checksums.txt`) and `URL` in `build/gowa.version`.
3. Delete `build/gowa/`, run `powershell -File build/fetch-gowa.ps1`, then `go test ./...`.
4. Smoke test: link a test number, then forward a text, an image and a document by WhatsApp and by email.
