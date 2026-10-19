package webhook

import (
	"encoding/json"
	"testing"
)

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
