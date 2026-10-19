// Package webhook receives GoWA events. It only verifies and stores them;
// all processing happens asynchronously in the engine, so GoWA never has to
// retry because of a slow rule.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"whatsapp-manager/internal/store"
)

const maxBody = 10 << 20

// Handler returns the /hook handler.
func Handler(st *store.Store, secret func() string, notify func(), log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil || len(body) > maxBody {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if !Verify(body, r.Header.Get("X-Hub-Signature-256"), secret()) {
			log.Warn("webhook signature mismatch")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var env struct {
			Event    string `json:"event"`
			DeviceID string `json:"device_id"`
			Payload  struct {
				ID        string `json:"id"`
				Timestamp string `json:"timestamp"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			// Malformed but authentic: acknowledge so GoWA does not retry forever.
			log.Warn("webhook: bad json", "err", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		if env.Event != "message" || env.Payload.ID == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		var msgTime int64
		if t, err := time.Parse(time.RFC3339, env.Payload.Timestamp); err == nil {
			msgTime = t.UnixMilli()
		}
		inserted, err := st.InsertInbox(r.Context(), env.DeviceID, env.Payload.ID, env.Event, msgTime, body)
		if err != nil {
			// Storage failed: let GoWA retry.
			log.Error("webhook: store failed", "err", err)
			http.Error(w, "store failed", http.StatusServiceUnavailable)
			return
		}
		if inserted {
			notify()
		} else {
			log.Debug("webhook: duplicate delivery ignored", "id", env.Payload.ID)
		}
		w.WriteHeader(http.StatusOK)
	})
}

// Verify checks a "sha256=<hex>" HMAC signature in constant time.
func Verify(body []byte, header, secret string) bool {
	if secret == "" || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// Sign returns the header value for body (used by tests and the fake server).
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
