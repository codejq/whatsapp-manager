// Package engine turns stored inbox events into outbox delivery jobs by
// evaluating the user's rules. Each inbox row is processed in one
// transaction, so processing is idempotent and crash-safe.
package engine

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"whatsapp-manager/internal/rules"
	"whatsapp-manager/internal/store"
)

// Reasons specific to the engine (i18n keys).
const (
	ReasonSentByUs  = "activity.reason.sent_by_us"
	ReasonPaused    = "activity.reason.paused"
	ReasonNoRules   = "activity.reason.no_rules"
	ReasonNoMatch   = "activity.reason.no_match"
	ReasonParse     = "activity.reason.parse_error"
	ReasonCooldown  = "activity.reason.cooldown"
	ReasonSameChat  = "activity.reason.same_chat"
	ReasonDuplicate = "activity.reason.duplicate"
)

// Options are read on every batch so settings changes apply immediately.
type Options struct {
	Paused bool
	MaxAge time.Duration
}

// Engine processes the inbox.
type Engine struct {
	Store   *store.Store
	Options func() Options
	Log     *slog.Logger
	// OnQueued is called after jobs were queued (wakes the dispatcher).
	OnQueued func()
	wake     chan struct{}
}

// New creates an engine.
func New(st *store.Store, opts func() Options, log *slog.Logger, onQueued func()) *Engine {
	return &Engine{Store: st, Options: opts, Log: log, OnQueued: onQueued, wake: make(chan struct{}, 1)}
}

// Notify wakes the engine (called by the webhook receiver).
func (e *Engine) Notify() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Run processes until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	for ctx.Err() == nil {
		n, err := e.ProcessBatch(ctx)
		if err != nil {
			e.Log.Error("engine batch failed", "err", err)
		}
		if n > 0 && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-e.wake:
		case <-time.After(3 * time.Second):
		}
	}
}

// ProcessBatch processes up to 50 new inbox rows and returns how many.
func (e *Engine) ProcessBatch(ctx context.Context) (int, error) {
	items, err := e.Store.NextNewInbox(ctx, 50)
	if err != nil || len(items) == 0 {
		return 0, err
	}
	all, err := e.Store.ListRules(ctx)
	if err != nil {
		return 0, err
	}
	opts := e.Options()
	queued := 0
	for _, it := range items {
		n, err := e.processOne(ctx, it, all, opts)
		if err != nil {
			e.Log.Error("engine: process failed", "inbox", it.ID, "err", err)
			_ = e.Store.MarkInbox(ctx, it.ID, "error", err.Error())
			continue
		}
		queued += n
	}
	if queued > 0 && e.OnQueued != nil {
		e.OnQueued()
	}
	return len(items), nil
}

func (e *Engine) processOne(ctx context.Context, it store.InboxItem, all []*rules.Rule, opts Options) (int, error) {
	defer func() {
		if r := recover(); r != nil {
			e.Log.Error("engine: panic", "inbox", it.ID, "panic", r)
			_ = e.Store.MarkInbox(ctx, it.ID, "error", "panic")
		}
	}()
	msg, err := rules.ParseWebhook([]byte(it.Payload))
	if err != nil {
		return 0, e.Store.MarkInbox(ctx, it.ID, "error", ReasonParse)
	}
	meta := e.Meta(ctx, msg)
	done := func(status, note string, matched []string, outs []store.NewOutbox) (int, error) {
		pr, err := e.Store.CompleteInbox(ctx, it, status, note, meta, matched, outs)
		if len(pr.Duplicates) > 0 {
			e.Log.Info("engine: destination already queued for message", "msg", msg.ID, "dups", pr.Duplicates)
		}
		return pr.Queued, err
	}

	if sent, _ := e.Store.IsSentByUs(ctx, msg.ID); sent {
		return done("ignored", ReasonSentByUs, nil, nil)
	}
	if opts.Paused {
		return done("ignored", ReasonPaused, nil, nil)
	}
	if len(all) == 0 {
		return done("ignored", ReasonNoRules, nil, nil)
	}

	ectx := rules.EvalContext{Now: time.Now(), MaxAge: opts.MaxAge}
	var (
		matched  []string
		outs     []store.NewOutbox
		seenDest = map[string]bool{}
		reasons  []string
	)
	for _, r := range all {
		res := rules.Evaluate(r, msg, ectx)
		if !res.Matched {
			if r.Enabled {
				reasons = append(reasons, res.Reason)
			}
			continue
		}
		if r.Options.CooldownSeconds > 0 {
			last, _ := e.Store.LastRuleFire(ctx, r.ID)
			if last > 0 && time.Since(time.UnixMilli(last)) < time.Duration(r.Options.CooldownSeconds)*time.Second {
				reasons = append(reasons, ReasonCooldown)
				continue
			}
		}
		matched = append(matched, r.Name)
		for _, a := range r.Actions {
			for _, o := range Expand(r, a, msg) {
				if seenDest[o.DestinationKey] {
					continue // an earlier rule already sends this message there
				}
				seenDest[o.DestinationKey] = true
				outs = append(outs, o)
			}
		}
	}
	if len(matched) == 0 {
		return done("ignored", pickReason(reasons), nil, nil)
	}
	return done("processed", "", matched, outs)
}

// Expand turns one action into outbox jobs (one per destination).
func Expand(r *rules.Rule, a rules.Action, msg *rules.Message) []store.NewOutbox {
	var outs []store.NewOutbox
	switch a.Type {
	case rules.ActionWhatsApp:
		if a.To == nil || a.To.JID == "" {
			return nil
		}
		to := strings.ToLower(strings.TrimSpace(a.To.JID))
		// Never send a message back into the chat it came from.
		if rules.PeerMatches(*a.To, msg.ChatID, msg.ChatLID) {
			return nil
		}
		label := a.To.Name
		if label == "" {
			label = rules.UserPart(to)
		}
		outs = append(outs, store.NewOutbox{RuleID: r.ID, RuleName: r.Name, Channel: "whatsapp", DestinationKey: "wa:" + to, DestinationLabel: label, Action: a})
	case rules.ActionEmail:
		for _, addr := range a.Emails {
			addr = strings.ToLower(strings.TrimSpace(addr))
			if addr == "" {
				continue
			}
			single := a
			single.Emails = []string{addr}
			outs = append(outs, store.NewOutbox{RuleID: r.ID, RuleName: r.Name, Channel: "email", DestinationKey: "mail:" + addr, DestinationLabel: addr, Action: single})
		}
	}
	return outs
}

// Meta builds the display summary of a message.
func (e *Engine) Meta(ctx context.Context, msg *rules.Message) store.InboxMeta {
	chatName := e.Store.ChatName(ctx, msg.ChatID)
	if chatName == "" && msg.IsPrivate() {
		chatName = msg.SenderName
		if msg.IsFromMe {
			chatName = rules.UserPart(msg.ChatID)
		}
	}
	if chatName == "" {
		chatName = rules.UserPart(msg.ChatID)
	}
	preview := msg.Body
	if preview == "" && msg.Media != nil {
		preview = firstNonEmpty(msg.Media.Caption, msg.Media.Filename)
	}
	return store.InboxMeta{
		ChatID: msg.ChatID, ChatName: chatName,
		Sender: msg.From, SenderName: msg.SenderName,
		MsgType: msg.Type, Preview: preview,
	}
}

func pickReason(reasons []string) string {
	if len(reasons) == 0 {
		return ReasonNoMatch
	}
	// Prefer the most informative reason: conditions > sender > source > time > own.
	order := []string{rules.ReasonConditions, rules.ReasonSender, ReasonCooldown, rules.ReasonSource, rules.ReasonBeforeRule, rules.ReasonTooOld, rules.ReasonOwnMessage}
	for _, o := range order {
		for _, r := range reasons {
			if r == o {
				return o
			}
		}
	}
	return reasons[0]
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
