package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"whatsapp-manager/internal/rules"
)

// ---------- settings ----------

// GetSetting reads a raw setting value; ok=false when missing.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetSetting upserts a raw setting value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetJSON decodes a JSON setting into dst; returns false if missing.
func (s *Store) GetJSON(ctx context.Context, key string, dst any) (bool, error) {
	v, ok, err := s.GetSetting(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	return true, json.Unmarshal([]byte(v), dst)
}

// SetJSON stores v as JSON.
func (s *Store) SetJSON(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, key, string(b))
}

// ---------- rules ----------

// ListRules returns all rules ordered by priority then creation.
func (s *Store) ListRules(ctx context.Context) ([]*rules.Rule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, enabled, priority, created_at, updated_at, def FROM rules ORDER BY priority ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*rules.Rule
	for rows.Next() {
		var (
			r       rules.Rule
			enabled int
			def     string
		)
		var id, name string
		var prio int
		var created, updated int64
		if err := rows.Scan(&id, &name, &enabled, &prio, &created, &updated, &def); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(def), &r); err != nil {
			continue // a corrupt rule must not break the others
		}
		r.ID, r.Name, r.Enabled, r.Priority, r.CreatedAt, r.UpdatedAt = id, name, enabled == 1, prio, created, updated
		out = append(out, &r)
	}
	return out, rows.Err()
}

// GetRule returns one rule.
func (s *Store) GetRule(ctx context.Context, id string) (*rules.Rule, error) {
	all, err := s.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, rules.ErrNotFound
}

// SaveRule inserts or updates a rule. CreatedAt is preserved on update.
func (s *Store) SaveRule(ctx context.Context, r *rules.Rule) error {
	now := NowMs()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	def, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO rules(id,name,enabled,priority,created_at,updated_at,def) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, enabled=excluded.enabled, priority=excluded.priority, updated_at=excluded.updated_at, def=excluded.def`,
		r.ID, r.Name, boolInt(r.Enabled), r.Priority, r.CreatedAt, r.UpdatedAt, string(def))
	return err
}

// SetRuleEnabled toggles a rule.
func (s *Store) SetRuleEnabled(ctx context.Context, id string, enabled bool) error {
	r, err := s.GetRule(ctx, id)
	if err != nil {
		return err
	}
	r.Enabled = enabled
	return s.SaveRule(ctx, r)
}

// DeleteRule removes a rule.
func (s *Store) DeleteRule(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return rules.ErrNotFound
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- inbox ----------

// InboxItem is one stored webhook event.
type InboxItem struct {
	ID         int64  `json:"id"`
	DeviceID   string `json:"device_id"`
	MessageID  string `json:"message_id"`
	Event      string `json:"event"`
	ReceivedAt int64  `json:"received_at"`
	MsgTime    int64  `json:"msg_time"`
	Status     string `json:"status"`
	Note       string `json:"note"`
	ChatID     string `json:"chat_id"`
	ChatName   string `json:"chat_name"`
	Sender     string `json:"sender"`
	SenderName string `json:"sender_name"`
	MsgType    string `json:"msg_type"`
	Preview    string `json:"preview"`
	Matched    string `json:"matched"`
	Payload    string `json:"-"`
}

// InsertInbox stores a raw event idempotently. inserted=false means the same
// (device, message, event) was already stored (a webhook retry).
func (s *Store) InsertInbox(ctx context.Context, deviceID, messageID, event string, msgTime int64, payload []byte) (inserted bool, err error) {
	res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO inbox(device_id,message_id,event,received_at,msg_time,payload) VALUES(?,?,?,?,?,?)`,
		deviceID, messageID, event, NowMs(), msgTime, string(payload))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// NextNewInbox returns up to limit unprocessed inbox rows, oldest first.
func (s *Store) NextNewInbox(ctx context.Context, limit int) ([]InboxItem, error) {
	return s.queryInbox(ctx, `WHERE status='new' ORDER BY id ASC LIMIT ?`, limit)
}

func (s *Store) queryInbox(ctx context.Context, where string, args ...any) ([]InboxItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,device_id,message_id,event,received_at,msg_time,status,note,chat_id,chat_name,sender,sender_name,msg_type,preview,matched,payload FROM inbox `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxItem
	for rows.Next() {
		var it InboxItem
		if err := rows.Scan(&it.ID, &it.DeviceID, &it.MessageID, &it.Event, &it.ReceivedAt, &it.MsgTime, &it.Status, &it.Note, &it.ChatID, &it.ChatName, &it.Sender, &it.SenderName, &it.MsgType, &it.Preview, &it.Matched, &it.Payload); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// InboxMeta is the display summary written when an inbox row is processed.
type InboxMeta struct {
	ChatID, ChatName, Sender, SenderName, MsgType, Preview string
}

// NewOutbox is an outbox row to create.
type NewOutbox struct {
	RuleID, RuleName  string
	Channel           string
	DestinationKey    string
	DestinationLabel  string
	Action            rules.Action
}

// ProcessResult reports what CompleteInbox did.
type ProcessResult struct {
	Queued     int
	Duplicates []string // destination keys already queued for this message
}

// CompleteInbox atomically writes the outbox rows for a message and marks the
// inbox row processed (or ignored). Because both happen in one transaction,
// a crash can never produce a processed message without its deliveries, or
// deliveries for a message that will be processed again.
func (s *Store) CompleteInbox(ctx context.Context, it InboxItem, status, note string, meta InboxMeta, matched []string, outs []NewOutbox) (ProcessResult, error) {
	var pr ProcessResult
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return pr, err
	}
	defer tx.Rollback()
	now := NowMs()
	for _, o := range outs {
		actJSON, _ := json.Marshal(o.Action)
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO outbox(inbox_id,device_id,message_id,rule_id,rule_name,channel,destination_key,destination_label,action,status,next_attempt_at,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?, 'pending', ?, ?, ?)`,
			it.ID, it.DeviceID, it.MessageID, o.RuleID, o.RuleName, o.Channel, o.DestinationKey, o.DestinationLabel, string(actJSON), now, now, now)
		if err != nil {
			return pr, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			pr.Queued++
		} else {
			pr.Duplicates = append(pr.Duplicates, o.DestinationKey)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE inbox SET status=?, note=?, chat_id=?, chat_name=?, sender=?, sender_name=?, msg_type=?, preview=?, matched=? WHERE id=? AND status='new'`,
		status, note, meta.ChatID, meta.ChatName, meta.Sender, meta.SenderName, meta.MsgType, truncate(meta.Preview, 500), strings.Join(matched, ", "), it.ID)
	if err != nil {
		return pr, err
	}
	return pr, tx.Commit()
}

// MarkInbox sets status/note on an inbox row (used for parse errors).
func (s *Store) MarkInbox(ctx context.Context, id int64, status, note string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE inbox SET status=?, note=? WHERE id=?`, status, note, id)
	return err
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ---------- outbox ----------

// OutboxItem is one delivery job.
type OutboxItem struct {
	ID               int64        `json:"id"`
	InboxID          int64        `json:"inbox_id"`
	DeviceID         string       `json:"device_id"`
	MessageID        string       `json:"message_id"`
	RuleID           string       `json:"rule_id"`
	RuleName         string       `json:"rule_name"`
	Channel          string       `json:"channel"`
	DestinationKey   string       `json:"destination_key"`
	DestinationLabel string       `json:"destination_label"`
	Action           rules.Action `json:"action"`
	Status           string       `json:"status"`
	Attempts         int          `json:"attempts"`
	NextAttemptAt    int64        `json:"next_attempt_at"`
	LastError        string       `json:"last_error"`
	SentMessageID    string       `json:"sent_message_id"`
	CreatedAt        int64        `json:"created_at"`
	UpdatedAt        int64        `json:"updated_at"`
}

const outboxCols = `id,inbox_id,device_id,message_id,rule_id,rule_name,channel,destination_key,destination_label,action,status,attempts,next_attempt_at,last_error,sent_message_id,created_at,updated_at`

func scanOutbox(rows *sql.Rows) (OutboxItem, error) {
	var o OutboxItem
	var act string
	err := rows.Scan(&o.ID, &o.InboxID, &o.DeviceID, &o.MessageID, &o.RuleID, &o.RuleName, &o.Channel, &o.DestinationKey, &o.DestinationLabel, &act, &o.Status, &o.Attempts, &o.NextAttemptAt, &o.LastError, &o.SentMessageID, &o.CreatedAt, &o.UpdatedAt)
	if err == nil {
		_ = json.Unmarshal([]byte(act), &o.Action)
	}
	return o, err
}

// ClaimNextOutbox atomically moves the oldest due pending job of a channel to
// "sending" and returns it. ok=false when nothing is due.
func (s *Store) ClaimNextOutbox(ctx context.Context, channel string) (OutboxItem, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return OutboxItem{}, false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+outboxCols+` FROM outbox WHERE status='pending' AND channel=? AND next_attempt_at<=? ORDER BY next_attempt_at ASC, id ASC LIMIT 1`, channel, NowMs())
	if err != nil {
		return OutboxItem{}, false, err
	}
	var it OutboxItem
	found := false
	if rows.Next() {
		it, err = scanOutbox(rows)
		found = err == nil
	}
	rows.Close()
	if err != nil || !found {
		return OutboxItem{}, false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE outbox SET status='sending', attempts=attempts+1, updated_at=? WHERE id=? AND status='pending'`, NowMs(), it.ID)
	if err != nil {
		return OutboxItem{}, false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return OutboxItem{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return OutboxItem{}, false, err
	}
	it.Status = "sending"
	it.Attempts++
	return it, true, nil
}

// MarkSent completes a job and remembers the sent message id (loop guard).
func (s *Store) MarkSent(ctx context.Context, id int64, sentMessageID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := NowMs()
	if _, err := tx.ExecContext(ctx, `UPDATE outbox SET status='sent', last_error='', sent_message_id=?, updated_at=? WHERE id=?`, sentMessageID, now, id); err != nil {
		return err
	}
	if sentMessageID != "" {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO sent_ids(message_id, created_at) VALUES(?,?)`, sentMessageID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Reschedule puts a job back to pending at `at`. If refund is true the attempt
// is not counted (used when the failure was not the job's fault, e.g. WhatsApp offline).
func (s *Store) Reschedule(ctx context.Context, id int64, at time.Time, lastErr string, refund bool) error {
	q := `UPDATE outbox SET status='pending', next_attempt_at=?, last_error=?, updated_at=? WHERE id=?`
	if refund {
		q = `UPDATE outbox SET status='pending', attempts=MAX(attempts-1,0), next_attempt_at=?, last_error=?, updated_at=? WHERE id=?`
	}
	_, err := s.DB.ExecContext(ctx, q, at.UnixMilli(), lastErr, NowMs(), id)
	return err
}

// MarkFailed marks a job permanently failed.
func (s *Store) MarkFailed(ctx context.Context, id int64, lastErr string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE outbox SET status='failed', last_error=?, updated_at=? WHERE id=?`, lastErr, NowMs(), id)
	return err
}

// MarkWaitingEmail parks an email job until SMTP works again; the attempt is refunded.
func (s *Store) MarkWaitingEmail(ctx context.Context, id int64, lastErr string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE outbox SET status='waiting_email', attempts=MAX(attempts-1,0), last_error=?, updated_at=? WHERE id=?`, lastErr, NowMs(), id)
	return err
}

// ResumeWaitingEmail moves all parked email jobs back to pending.
func (s *Store) ResumeWaitingEmail(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE outbox SET status='pending', next_attempt_at=?, updated_at=? WHERE status='waiting_email'`, NowMs(), NowMs())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RecoverInFlight runs at startup: jobs left in "sending" may or may not have
// been delivered before the crash, so they become "uncertain" and are never
// re-sent automatically (at-most-once).
func (s *Store) RecoverInFlight(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE outbox SET status='uncertain', last_error='activity.reason.interrupted', updated_at=? WHERE status='sending'`, NowMs())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RetryOutbox re-queues a failed/uncertain/cancelled job on user request.
func (s *Store) RetryOutbox(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE outbox SET status='pending', attempts=0, next_attempt_at=?, last_error='', updated_at=? WHERE id=? AND status IN ('failed','uncertain','cancelled','waiting_email')`, NowMs(), NowMs(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("not retryable")
	}
	return nil
}

// CancelOutbox cancels a pending/failed job on user request.
func (s *Store) CancelOutbox(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE outbox SET status='cancelled', updated_at=? WHERE id=? AND status IN ('pending','failed','uncertain','waiting_email')`, NowMs(), id)
	return err
}

// GetOutbox fetches one job.
func (s *Store) GetOutbox(ctx context.Context, id int64) (OutboxItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+outboxCols+` FROM outbox WHERE id=?`, id)
	if err != nil {
		return OutboxItem{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return OutboxItem{}, sql.ErrNoRows
	}
	return scanOutbox(rows)
}

// IsSentByUs reports whether a message id was sent by this app.
func (s *Store) IsSentByUs(ctx context.Context, messageID string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM sent_ids WHERE message_id=?`, messageID).Scan(&n)
	return n > 0, err
}

// LastRuleFire returns the last time a rule queued anything (cooldown).
func (s *Store) LastRuleFire(ctx context.Context, ruleID string) (int64, error) {
	var t sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT MAX(created_at) FROM outbox WHERE rule_id=?`, ruleID).Scan(&t)
	return t.Int64, err
}

// GetInboxPayload returns the raw payload of an inbox row.
func (s *Store) GetInboxPayload(ctx context.Context, id int64) (string, error) {
	var p string
	err := s.DB.QueryRowContext(ctx, `SELECT payload FROM inbox WHERE id=?`, id).Scan(&p)
	return p, err
}

// ---------- activity & stats ----------

// ActivityEntry is an inbox row with its deliveries.
type ActivityEntry struct {
	InboxItem
	Deliveries []OutboxItem `json:"deliveries"`
}

// ActivityFilter narrows the activity list.
type ActivityFilter struct {
	Limit    int
	BeforeID int64
	Only     string // "" | matched | failed
	Query    string
}

// Activity returns recent inbox rows with their deliveries.
func (s *Store) Activity(ctx context.Context, f ActivityFilter) ([]ActivityEntry, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	where := []string{"1=1"}
	var args []any
	if f.BeforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, f.BeforeID)
	}
	switch f.Only {
	case "matched":
		where = append(where, "matched <> ''")
	case "failed":
		where = append(where, "id IN (SELECT inbox_id FROM outbox WHERE status IN ('failed','uncertain','waiting_email'))")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "(preview LIKE ? OR chat_name LIKE ? OR sender_name LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	args = append(args, f.Limit)
	items, err := s.queryInbox(ctx, `WHERE `+strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	out := make([]ActivityEntry, 0, len(items))
	for _, it := range items {
		it.Payload = ""
		e := ActivityEntry{InboxItem: it, Deliveries: []OutboxItem{}}
		rows, err := s.DB.QueryContext(ctx, `SELECT `+outboxCols+` FROM outbox WHERE inbox_id=? ORDER BY id`, it.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			o, err := scanOutbox(rows)
			if err == nil {
				e.Deliveries = append(e.Deliveries, o)
			}
		}
		rows.Close()
		out = append(out, e)
	}
	return out, nil
}

// Stats are counts since a point in time.
type Stats struct {
	Received  int `json:"received"`
	Matched   int `json:"matched"`
	Sent      int `json:"sent"`
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Uncertain int `json:"uncertain"`
	Waiting   int `json:"waiting_email"`
}

// StatsSince computes dashboard counters.
func (s *Store) StatsSince(ctx context.Context, since time.Time) (Stats, error) {
	var st Stats
	ms := since.UnixMilli()
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1), COALESCE(SUM(CASE WHEN matched<>'' THEN 1 ELSE 0 END),0) FROM inbox WHERE received_at>=?`, ms).Scan(&st.Received, &st.Matched); err != nil {
		return st, err
	}
	err := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN status='sent' AND updated_at>=? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' AND updated_at>=? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status IN ('pending','sending') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='uncertain' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='waiting_email' THEN 1 ELSE 0 END),0)
		FROM outbox`, ms, ms).Scan(&st.Sent, &st.Failed, &st.Pending, &st.Uncertain, &st.Waiting)
	return st, err
}

// ---------- chats cache ----------

// Chat is a cached group/contact name.
type Chat struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// UpsertChats replaces cached names for the given chats.
func (s *Store) UpsertChats(ctx context.Context, chats []Chat) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := NowMs()
	for _, c := range chats {
		if c.JID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO chats(jid,name,kind,updated_at) VALUES(?,?,?,?) ON CONFLICT(jid) DO UPDATE SET name=excluded.name, kind=excluded.kind, updated_at=excluded.updated_at`, c.JID, c.Name, c.Kind, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListChats returns cached chats of a kind ("" for all).
func (s *Store) ListChats(ctx context.Context, kind string) ([]Chat, error) {
	q := `SELECT jid,name,kind FROM chats`
	var args []any
	if kind != "" {
		q += ` WHERE kind=?`
		args = append(args, kind)
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chat{}
	for rows.Next() {
		var c Chat
		if err := rows.Scan(&c.JID, &c.Name, &c.Kind); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChatName returns a cached chat name or "".
func (s *Store) ChatName(ctx context.Context, jid string) string {
	var n string
	_ = s.DB.QueryRowContext(ctx, `SELECT name FROM chats WHERE jid=?`, jid).Scan(&n)
	return n
}

// ---------- maintenance ----------

// Purge deletes finished history older than the retention window.
func (s *Store) Purge(ctx context.Context, olderThan time.Duration) error {
	cut := time.Now().Add(-olderThan).UnixMilli()
	stmts := []string{
		`DELETE FROM outbox WHERE updated_at < ? AND status IN ('sent','failed','cancelled','uncertain')`,
		`DELETE FROM inbox WHERE received_at < ? AND status <> 'new' AND id NOT IN (SELECT inbox_id FROM outbox)`,
		`DELETE FROM sent_ids WHERE created_at < ?`,
	}
	for _, q := range stmts {
		if _, err := s.DB.ExecContext(ctx, q, cut); err != nil {
			return err
		}
	}
	return nil
}
