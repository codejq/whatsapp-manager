package gowa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func fakeGowa(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "wam" || p != "pw" {
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("X-Device-Id") != DeviceID {
			w.WriteHeader(400)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/app/status":
			w.Write([]byte(`{"code":"SUCCESS","results":{"is_connected":true,"is_logged_in":true,"jid":"966500000000@s.whatsapp.net"}}`))
		case r.URL.Path == "/user/my/groups":
			w.Write([]byte(`{"code":"SUCCESS","results":{"data":[{"JID":"1@g.us","Name":"Sales"}]}}`))
		case r.URL.Path == "/user/my/contacts":
			w.Write([]byte(`{"code":"SUCCESS","results":{"data":[{"jid":"9665@s.whatsapp.net","name":"Ali"}]}}`))
		case strings.HasSuffix(r.URL.Path, "/forward"):
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["phone"] == "" {
				w.WriteHeader(400)
				w.Write([]byte(`{"code":"BAD_REQUEST","message":"phone required"}`))
				return
			}
			w.Write([]byte(`{"code":"SUCCESS","results":{"message_id":"FWD1","status":"ok"}}`))
		case r.URL.Path == "/send/file":
			if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("phone") == "" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`{"code":"SUCCESS","results":{"message_id":"FILE1"}}`))
		case r.URL.Path == "/send/message":
			w.WriteHeader(500)
			w.Write([]byte(`{"code":"AUTHENTICATION_ERROR","message":"you are not logged in"}`))
		case r.URL.Path == "/devices":
			w.WriteHeader(500)
			w.Write([]byte(`{"code":"INTERNAL_SERVER_ERROR","message":"device main already exists"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	return NewClient(port, "wam", "pw"), &calls
}

func TestClient(t *testing.T) {
	c, _ := fakeGowa(t)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil || !st.IsConnected || !Paired(st) {
		t.Fatalf("status %+v err=%v", st, err)
	}
	if !Paired(Status{JID: "x@s.whatsapp.net"}) || Paired(Status{}) {
		t.Fatal("Paired logic wrong")
	}
	g, err := c.Groups(ctx)
	if err != nil || len(g) != 1 || g[0].Name != "Sales" || g[0].JID != "1@g.us" {
		t.Fatalf("groups %+v err=%v", g, err)
	}
	ct, err := c.Contacts(ctx)
	if err != nil || len(ct) != 1 || ct[0].Name != "Ali" {
		t.Fatalf("contacts %+v err=%v", ct, err)
	}
	id, err := c.Forward(ctx, "MSG1", "9665@s.whatsapp.net")
	if err != nil || id != "FWD1" {
		t.Fatalf("forward id=%s err=%v", id, err)
	}
	if err := c.EnsureDevice(ctx); err != nil {
		t.Fatalf("already-exists must be ok: %v", err)
	}
	_, err = c.SendText(ctx, "1@s.whatsapp.net", "hi")
	if !errors.Is(err, ErrNotLoggedIn) || !IsTransient(err) {
		t.Fatalf("want ErrNotLoggedIn, got %v", err)
	}
	_, err = c.Forward(ctx, "MSG1", "")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 || IsTransient(err) {
		t.Fatalf("want permanent 400, got %v", err)
	}
}

func TestUnavailable(t *testing.T) {
	c := NewClient(1, "u", "p") // nothing listens on port 1
	_, err := c.Status(context.Background())
	if !errors.Is(err, ErrUnavailable) || !IsTransient(err) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestFetchQRRejectsForeignLinks(t *testing.T) {
	c, _ := fakeGowa(t)
	if _, err := c.FetchQR(context.Background(), "http://evil.example/statics/qrcode/x.png"); err == nil {
		t.Fatal("foreign QR link accepted")
	}
}
