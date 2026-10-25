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
const AppName = "WhatsAppManager"

// DataDir returns %LOCALAPPDATA%\WhatsAppManager (created if missing).
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

var dpapiEntropy = []byte("WhatsAppManager/v1")

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
	if err != nil {
		return false
	}
	want, err := autostartCommand()
	return err == nil && strings.EqualFold(v, want)
}

func autostartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`"%s" --minimized`, exe), nil
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
	cmd, err := autostartCommand()
	if err != nil {
		return err
	}
	return k.SetStringValue(AppName, cmd)
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
