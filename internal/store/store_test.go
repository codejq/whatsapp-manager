package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"whatsapp-manager/internal/rules"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInboxIdempotent(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	ok1, err := s.InsertInbox(ctx, "d", "m1", "message", 0, []byte(`{}`))
	if err != nil || !ok1 {
		t.Fatal("first insert should succeed", err)
	}
	ok2, err := s.InsertInbox(ctx, "d", "m1", "message", 0, []byte(`{}`))
	if err != nil || ok2 {
		t.Fatal("duplicate insert must be ignored", err)
	}
}

func TestOutboxDedupAcrossRules(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	s.InsertInbox(ctx, "d", "m1", "message", 0, []byte(`{}`))
	items, _ := s.NextNewInbox(ctx, 10)
	if len(items) != 1 {
		t.Fatal("expected 1 new item")
	}
	outs := []NewOutbox{
		{RuleID: "r1", RuleName: "R1", Channel: "email", DestinationKey: "mail:a@b.com", Action: rules.Action{Type: "email"}},
		{RuleID: "r2", RuleName: "R2", Channel: "email", DestinationKey: "mail:a@b.com", Action: rules.Action{Type: "email"}},
		{RuleID: "r2", RuleName: "R2", Channel: "whatsapp", DestinationKey: "wa:1@s.whatsapp.net", Action: rules.Action{Type: "whatsapp"}},
	}
	pr, err := s.CompleteInbox(ctx, items[0], "processed", "", InboxMeta{}, []string{"R1", "R2"}, outs)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Queued != 2 || len(pr.Duplicates) != 1 {
		t.Fatalf("queued=%d dups=%v", pr.Queued, pr.Duplicates)
	}
	// Processing the same inbox row again (e.g. after a crash) must not queue anything.
	pr2, err := s.CompleteInbox(ctx, items[0], "processed", "", InboxMeta{}, nil, outs)
	if err != nil || pr2.Queued != 0 {
		t.Fatalf("reprocess queued=%d err=%v", pr2.Queued, err)
	}
	if left, _ := s.NextNewInbox(ctx, 10); len(left) != 0 {
		t.Fatal("inbox should be empty")
	}
}

func TestOutboxStateMachine(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	s.InsertInbox(ctx, "d", "m1", "message", 0, []byte(`{}`))
	items, _ := s.NextNewInbox(ctx, 10)
	s.CompleteInbox(ctx, items[0], "processed", "", InboxMeta{}, nil, []NewOutbox{
		{RuleID: "r1", Channel: "whatsapp", DestinationKey: "wa:x", Action: rules.Action{Type: "whatsapp"}},
	})
	job, ok, err := s.ClaimNextOutbox(ctx, "whatsapp")
	if err != nil || !ok || job.Attempts != 1 {
		t.Fatalf("claim: ok=%v err=%v job=%+v", ok, err, job)
	}
	if _, ok, _ := s.ClaimNextOutbox(ctx, "whatsapp"); ok {
		t.Fatal("a job in 'sending' must not be claimed twice")
	}
	// Simulate crash while sending.
	n, err := s.RecoverInFlight(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recover n=%d err=%v", n, err)
	}
	got, _ := s.GetOutbox(ctx, job.ID)
	if got.Status != "uncertain" {
		t.Fatalf("status=%s want uncertain", got.Status)
	}
	if _, ok, _ := s.ClaimNextOutbox(ctx, "whatsapp"); ok {
		t.Fatal("uncertain jobs must not be auto-sent")
	}
	if err := s.RetryOutbox(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	job, ok, _ = s.ClaimNextOutbox(ctx, "whatsapp")
	if !ok {
		t.Fatal("retried job should be claimable")
	}
	if err := s.MarkSent(ctx, job.ID, "SENT1"); err != nil {
		t.Fatal(err)
	}
	if yes, _ := s.IsSentByUs(ctx, "SENT1"); !yes {
		t.Fatal("sent id should be recorded")
	}
	// Reschedule in the future: not claimable now.
	s.InsertInbox(ctx, "d", "m2", "message", 0, []byte(`{}`))
	items, _ = s.NextNewInbox(ctx, 10)
	s.CompleteInbox(ctx, items[0], "processed", "", InboxMeta{}, nil, []NewOutbox{{RuleID: "r1", Channel: "whatsapp", DestinationKey: "wa:x", Action: rules.Action{Type: "whatsapp"}}})
	job, _, _ = s.ClaimNextOutbox(ctx, "whatsapp")
	s.Reschedule(ctx, job.ID, time.Now().Add(time.Hour), "offline", true)
	if _, ok, _ := s.ClaimNextOutbox(ctx, "whatsapp"); ok {
		t.Fatal("future job claimed early")
	}
	got, _ = s.GetOutbox(ctx, job.ID)
	if got.Attempts != 0 {
		t.Fatalf("refunded attempts=%d want 0", got.Attempts)
	}
}

func TestRulesCRUDAndBackup(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	r := &rules.Rule{ID: "a", Name: "Rule A", Enabled: true, Source: rules.Source{Kind: "any"}}
	if err := s.SaveRule(ctx, r); err != nil {
		t.Fatal(err)
	}
	created := r.CreatedAt
	r.Name = "Rule A2"
	s.SaveRule(ctx, r)
	got, err := s.GetRule(ctx, "a")
	if err != nil || got.Name != "Rule A2" || got.CreatedAt != created {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if err := s.SetRuleEnabled(ctx, "a", false); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetRule(ctx, "a")
	if got.Enabled {
		t.Fatal("should be disabled")
	}
	dir := t.TempDir()
	if _, err := s.Backup(ctx, dir, 2); err != nil {
		t.Fatal(err)
	}
	if LatestBackup(dir) == "" {
		t.Fatal("no backup")
	}
	if err := s.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRule(ctx, "a"); err != nil {
		t.Fatal(err)
	}
}

func TestNextCounter(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for want := int64(1); want <= 3; want++ {
		n, err := s.NextCounter(ctx, "c")
		if err != nil || n != want {
			t.Fatalf("NextCounter = %d, %v; want %d", n, err, want)
		}
	}
}

func TestPurgeKeepsPendingAndRecent(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	old := time.Now().Add(-10 * 24 * time.Hour).UnixMilli()
	defer func(f func() int64) { NowMs = f }(NowMs)
	NowMs = func() int64 { return old }
	for _, id := range []string{"old-sent", "old-pending", "old-ignored"} {
		s.InsertInbox(ctx, "d", id, "message", 0, []byte(`{}`))
	}
	items, _ := s.NextNewInbox(ctx, 10)
	for _, it := range items {
		switch it.MessageID {
		case "old-sent", "old-pending":
			s.CompleteInbox(ctx, it, "processed", "", InboxMeta{}, nil, []NewOutbox{{RuleID: "r", Channel: "email", DestinationKey: "mail:" + it.MessageID, Action: rules.Action{Type: "email"}}})
		default:
			s.CompleteInbox(ctx, it, "ignored", "", InboxMeta{}, nil, nil)
		}
	}
	job, _, _ := s.ClaimNextOutbox(ctx, "email")
	if job.MessageID == "old-sent" {
		s.MarkSent(ctx, job.ID, "SENT-OLD")
	} else {
		s.Reschedule(ctx, job.ID, time.Now(), "", true)
		job, _, _ = s.ClaimNextOutbox(ctx, "email")
		s.MarkSent(ctx, job.ID, "SENT-OLD")
	}
	NowMs = func() int64 { return time.Now().UnixMilli() }
	s.InsertInbox(ctx, "d", "recent", "message", 0, []byte(`{}`))

	pr, err := s.Purge(ctx, 5*24*time.Hour, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Inbox != 2 || pr.Outbox != 1 {
		t.Fatalf("purged inbox=%d outbox=%d, want 2 and 1", pr.Inbox, pr.Outbox)
	}
	acts, _ := s.Activity(ctx, ActivityFilter{})
	left := map[string]bool{}
	for _, a := range acts {
		left[a.MessageID] = true
	}
	if !left["old-pending"] || !left["recent"] || len(left) != 2 {
		t.Fatalf("unexpected rows left: %v", left)
	}
	// The loop-guard ID is still within its 14-day window.
	if yes, _ := s.IsSentByUs(ctx, "SENT-OLD"); !yes {
		t.Fatal("sent id purged too early")
	}
}
