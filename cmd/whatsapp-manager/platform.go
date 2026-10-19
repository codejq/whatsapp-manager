//go:build windows

package main

import (
	"sync"
	"syscall"

	"whatsapp-manager/internal/winx"
)

// platform implements app.Platform with Windows APIs.
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

func (p *platform) Assign(pid int) error {
	p.jobOnce.Do(func() { p.job, p.jobErr = winx.NewKillOnCloseJob() })
	if p.jobErr != nil {
		return p.jobErr
	}
	return p.job.Assign(pid)
}
