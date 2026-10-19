package webhook

import "testing"

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
