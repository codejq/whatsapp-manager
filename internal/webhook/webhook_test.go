package webhook

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// GoWA sends each webhook with a new, never-closed http.Transport. The server
// must close every connection itself, or one socket leaks per message.
func TestHandlerClosesConnections(t *testing.T) {
	var closed atomic.Int32
	srv := httptest.NewUnstartedServer(Handler(nil, func() string { return "" }, func() {}, nil))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateClosed {
			closed.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	const n = 5
	for i := 0; i < n; i++ {
		cl := &http.Client{Transport: &http.Transport{}} // like GoWA: one per webhook
		resp, err := cl.Get(srv.URL)                     // GET: answered without touching the store
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	deadline := time.Now().Add(2 * time.Second)
	for closed.Load() < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := closed.Load(); got != n {
		t.Fatalf("closed %d of %d connections; the rest leak", got, n)
	}
}

func TestHasContent(t *testing.T) {
	cases := map[string]bool{
		`{"id":"x","chat_id":"g","from":"f"}`:      false,
		`{"id":"x","body":""}`:                     false,
		`{"id":"x","body":"hi"}`:                   true,
		`{"id":"x","image":"statics/media/a.jpg"}`: true,
		`{"id":"x","location":{"lat":1}}`:          true,
	}
	for in, want := range cases {
		var p map[string]json.RawMessage
		json.Unmarshal([]byte(in), &p)
		if got := HasContent(p); got != want {
			t.Errorf("HasContent(%s) = %v, want %v", in, got, want)
		}
	}
}

func TestVerify(t *testing.T) {
	body := []byte(`{"event":"message"}`)
	sig := Sign(body, "s")
	if !Verify(body, sig, "s") {
		t.Fatal("valid signature rejected")
	}
	if Verify(body, sig, "other") || Verify(body, "sha256=zz", "s") || Verify(body, "", "s") || Verify(body, sig, "") {
		t.Fatal("invalid signature accepted")
	}
	if Verify([]byte(`{"event":"x"}`), sig, "s") {
		t.Fatal("tampered body accepted")
	}
}
