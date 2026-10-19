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
	a.cleanup(ctx)
	a.autoCheckGowa(ctx)
	if a.Super != nil && a.Super.State().WA == "connected" {
		_ = a.refreshChats(ctx)
	}
}

// cleanup deletes messages, history and downloaded media older than the
// cleanup period set in Settings.
func (a *App) cleanup(ctx context.Context) {
	g := a.General()
	period := time.Duration(g.RetentionDays) * 24 * time.Hour
	// Sent-message IDs (loop guard) must outlive the catch-up window.
	sentIDs := time.Duration(g.CatchupHours)*time.Hour + 48*time.Hour
	pr, err := a.Store.Purge(ctx, period, sentIDs)
	if err != nil {
		a.Log.Error("cleanup failed", "err", err)
		return
	}
	if pr.Inbox > 0 || pr.Outbox > 0 {
		a.Log.Info("cleanup removed old messages", "messages", pr.Inbox, "deliveries", pr.Outbox, "older_than_days", g.RetentionDays)
	}
	removeOlder(filepath.Join(a.gowaDir, "statics", "media"), period)
	removeOlder(filepath.Join(a.gowaDir, "statics", "qrcode"), 24*time.Hour)
	removeOlder(filepath.Join(a.gowaDir, "statics", "senditems"), 24*time.Hour)
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
