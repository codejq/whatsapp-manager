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
const AppUserModelID = "WhatsAppManager.Desktop"

const powershellAUMID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

func toastAppID() string {
	if IsPackaged() {
		return "" // packaged apps cannot be targeted from PowerShell reliably; use fallback ID
	}
	sc := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, "WhatsApp Manager.lnk")
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
