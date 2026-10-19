package gowa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Phases of the GoWA process.
const (
	PhaseStarting = "starting"
	PhaseRunning  = "running"
	PhaseStopped  = "stopped"
	PhaseError    = "error"
)

// WhatsApp states.
const (
	WAUnknown      = "unknown"
	WALoggedOut    = "logged_out"
	WAConnected    = "connected"
	WADisconnected = "disconnected"
)

// State is a snapshot for the UI and tray.
type State struct {
	Phase     string    `json:"phase"`
	WA        string    `json:"wa"`
	JID       string    `json:"jid"`
	LastError string    `json:"last_error,omitempty"`
	Since     time.Time `json:"since"`
	Restarts  int       `json:"restarts"`
}

// Config configures the supervisor.
type Config struct {
	ExePath       string
	WorkDir       string
	Port          int
	User, Pass    string
	WebhookURL    string
	WebhookSecret string
	Log           io.Writer // child stdout/stderr
	Logger        *slog.Logger
	// ProcAttr returns process attributes (hide console window).
	ProcAttr func() *syscall.SysProcAttr
	// Assign puts the started process into a kill-on-close job.
	Assign func(pid int) error
	// OnPortChange persists a new port when the old one is taken.
	OnPortChange func(port int)
}

// Supervisor runs and watches GoWA.
type Supervisor struct {
	cfg Config

	mu       sync.RWMutex
	state    State
	client   *Client
	subs     []func(State)
	kick     chan struct{}
	restart  chan struct{}
	lastWAOK time.Time
}

// NewSupervisor creates a supervisor; call Run to start.
func NewSupervisor(cfg Config) *Supervisor {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &Supervisor{
		cfg:     cfg,
		state:   State{Phase: PhaseStarting, WA: WAUnknown, Since: time.Now()},
		kick:    make(chan struct{}, 1),
		restart: make(chan struct{}, 1),
	}
	s.client = NewClient(cfg.Port, cfg.User, cfg.Pass)
	return s
}

// Client returns the current REST client.
func (s *Supervisor) Client() *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

// State returns the current state.
func (s *Supervisor) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Subscribe registers a callback for state changes.
func (s *Supervisor) Subscribe(fn func(State)) {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	s.mu.Unlock()
}

// Kick asks for an immediate status check (e.g. after login, resume).
func (s *Supervisor) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Restart asks the supervisor to restart GoWA.
func (s *Supervisor) Restart() {
	select {
	case s.restart <- struct{}{}:
	default:
	}
}

func (s *Supervisor) update(fn func(*State)) {
	s.mu.Lock()
	old := s.state
	fn(&s.state)
	if s.state.Phase != old.Phase || s.state.WA != old.WA {
		s.state.Since = time.Now()
	}
	st := s.state
	subs := append([]func(State){}, s.subs...)
	changed := st.Phase != old.Phase || st.WA != old.WA || st.JID != old.JID || st.LastError != old.LastError
	s.mu.Unlock()
	if changed {
		for _, fn := range subs {
			fn(st)
		}
	}
}

// Run supervises GoWA until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		if _, err := os.Stat(s.cfg.ExePath); err != nil {
			s.update(func(st *State) { st.Phase = PhaseError; st.LastError = "errors.gowa_missing" })
			s.cfg.Logger.Error("gowa binary missing", "path", s.cfg.ExePath)
			if !sleepCtx(ctx, 30*time.Second) {
				return
			}
			continue
		}
		s.ensurePort()
		started := time.Now()
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			s.update(func(st *State) { st.Phase = PhaseStopped })
			return
		}
		if time.Since(started) > 5*time.Minute {
			backoff = time.Second
		}
		s.cfg.Logger.Warn("gowa exited; restarting", "err", err, "backoff", backoff)
		s.update(func(st *State) {
			st.Phase = PhaseStarting
			st.WA = WAUnknown
			st.Restarts++
			if err != nil {
				st.LastError = err.Error()
			}
		})
		if !sleepCtx(ctx, backoff) {
			return
		}
		backoff *= 2
		if backoff > time.Minute {
			backoff = time.Minute
		}
	}
}

func (s *Supervisor) ensurePort() {
	if portFree(s.cfg.Port) {
		return
	}
	p, err := FreePort()
	if err != nil {
		return
	}
	s.cfg.Logger.Warn("gowa port in use; switching", "old", s.cfg.Port, "new", p)
	s.cfg.Port = p
	s.mu.Lock()
	s.client = NewClient(p, s.cfg.User, s.cfg.Pass)
	s.mu.Unlock()
	if s.cfg.OnPortChange != nil {
		s.cfg.OnPortChange(p)
	}
}

func (s *Supervisor) args() []string {
	return []string{
		"rest",
		"--host=127.0.0.1",
		"--port=" + strconv.Itoa(s.cfg.Port),
		"--basic-auth=" + s.cfg.User + ":" + s.cfg.Pass,
		"--webhook=" + s.cfg.WebhookURL,
		"--webhook-secret=" + s.cfg.WebhookSecret,
		"--webhook-events=message",
		"--ui-enabled=false",
		"--ui-auto-update=false",
		"--mcp-enabled=false",
		"--os=WhatsApp Manager",
		"--auto-download-media=true",
		"--ignore-status-media=true",
		"--presence-on-connect=unavailable",
	}
}

func (s *Supervisor) runOnce(ctx context.Context) error {
	if err := os.MkdirAll(s.cfg.WorkDir, 0o700); err != nil {
		return err
	}
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(pctx, s.cfg.ExePath, s.args()...)
	cmd.Dir = s.cfg.WorkDir
	if s.cfg.Log != nil {
		cmd.Stdout = s.cfg.Log
		cmd.Stderr = s.cfg.Log
	}
	if s.cfg.ProcAttr != nil {
		cmd.SysProcAttr = s.cfg.ProcAttr()
	}
	cmd.WaitDelay = 5 * time.Second
	s.update(func(st *State) { st.Phase = PhaseStarting })
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if s.cfg.Assign != nil {
		if err := s.cfg.Assign(cmd.Process.Pid); err != nil {
			s.cfg.Logger.Warn("assign job object failed", "err", err)
		}
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// Wait for /health.
	client := s.Client()
	deadline := time.Now().Add(45 * time.Second)
	for {
		hctx, hcancel := context.WithTimeout(ctx, 2*time.Second)
		err := client.Health(hctx)
		hcancel()
		if err == nil {
			break
		}
		select {
		case e := <-exited:
			return fmt.Errorf("exited during startup: %v", e)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			cancel()
			<-exited
			return errors.New("health check timeout")
		}
	}
	if err := client.EnsureDevice(ctx); err != nil {
		s.cfg.Logger.Warn("ensure device failed", "err", err)
	}
	s.update(func(st *State) { st.Phase = PhaseRunning; st.LastError = "" })
	s.cfg.Logger.Info("gowa running", "pid", cmd.Process.Pid, "port", s.cfg.Port)

	// Monitor loop.
	s.poll(ctx)
	lastTick := time.Now()
	lastReconnect := time.Time{}
	for {
		interval := 15 * time.Second
		if st := s.State(); st.WA != WAConnected {
			interval = 4 * time.Second
		}
		select {
		case e := <-exited:
			return fmt.Errorf("exited: %v", e)
		case <-ctx.Done():
			cancel()
			<-exited
			return ctx.Err()
		case <-s.restart:
			cancel()
			<-exited
			return errors.New("restart requested")
		case <-s.kick:
		case <-time.After(interval):
		}
		// A long gap between ticks means the PC slept: force a reconnect check.
		resumed := time.Since(lastTick) > interval*2+10*time.Second
		lastTick = time.Now()
		st := s.poll(ctx)
		if Paired(st) && !st.IsConnected {
			if resumed || time.Since(lastReconnect) > 60*time.Second && time.Since(s.lastWAOK) > 30*time.Second {
				lastReconnect = time.Now()
				s.cfg.Logger.Info("reconnecting whatsapp", "resumed", resumed)
				rctx, rcancel := context.WithTimeout(ctx, 30*time.Second)
				_ = client.Reconnect(rctx)
				rcancel()
			}
		}
	}
}

func (s *Supervisor) poll(ctx context.Context) Status {
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	st, err := s.Client().Status(pctx)
	if err != nil {
		if errors.Is(err, ErrNotLoggedIn) {
			s.update(func(x *State) { x.WA = WALoggedOut; x.JID = "" })
		}
		return st
	}
	s.update(func(x *State) {
		switch {
		case st.IsLoggedIn && st.IsConnected:
			x.WA = WAConnected
			s.lastWAOK = time.Now()
		case Paired(st):
			// whatsmeow reports is_logged_in=false while the socket is down,
			// but a stored device JID means the session still exists.
			x.WA = WADisconnected
		default:
			x.WA = WALoggedOut
		}
		x.JID = st.JID
	})
	return st
}

// Paired reports whether a WhatsApp session exists (even if disconnected).
func Paired(st Status) bool { return st.IsLoggedIn || st.JID != "" }

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func portFree(port int) bool {
	if port <= 0 {
		return false
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// FreePort returns a currently free loopback port.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
