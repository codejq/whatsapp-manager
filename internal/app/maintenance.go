package app

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func procAttr(p Platform) func() *syscall.SysProcAttr {
	return func() *syscall.SysProcAttr { return p.ProcAttr() }
}

// maintenance runs hourly: daily backup, retention purge, media cleanup and
// a periodic refresh of chat names.
func (a *App) maintenance(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	first := time.After(2 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-first:
		case <-tick.C:
		}
		a.runMaintenance(ctx)
	}
}

func (a *App) runMaintenance(ctx context.Context) {
	g := a.General()
	var last time.Time
	_, _ = a.Store.GetJSON(ctx, keyLastBackup, &last)
	if time.Since(last) > 24*time.Hour {
		if name, err := a.Store.Backup(ctx, filepath.Join(a.cfg.DataDir, "backups"), 7); err != nil {
			a.Log.Error("backup failed", "err", err)
		} else {
			_ = a.Store.SetJSON(ctx, keyLastBackup, time.Now())
			a.Log.Info("backup written", "file", name)
		}
	}
	if err := a.Store.Purge(ctx, time.Duration(g.RetentionDays)*24*time.Hour); err != nil {
		a.Log.Error("purge failed", "err", err)
	}
	removeOlder(filepath.Join(a.gowaDir, "statics", "media"), time.Duration(g.MediaRetentionDays)*24*time.Hour)
	removeOlder(filepath.Join(a.gowaDir, "statics", "qrcode"), 24*time.Hour)
	removeOlder(filepath.Join(a.gowaDir, "statics", "senditems"), 24*time.Hour)
	if a.Super != nil && a.Super.State().WA == "connected" {
		_ = a.refreshChats(ctx)
	}
}

func removeOlder(dir string, age time.Duration) {
	cut := time.Now().Add(-age)
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(cut) {
			_ = os.Remove(path)
		}
		return nil
	})
}
