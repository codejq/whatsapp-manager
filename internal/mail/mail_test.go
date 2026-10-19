package mail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	s := Settings{Host: "smtp.example.com", FromAddress: "a@example.com"}
	s.Normalize()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Port != 587 || s.Username != "a@example.com" {
		t.Fatalf("defaults not applied: %+v", s)
	}
	s.Security = SecNone
	if err := s.Validate(); err == nil || err.Error() != "errors.smtp.insecure_auth" {
		t.Fatalf("expected insecure auth error, got %v", err)
	}
	s.AllowInsecureAuth = true
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := Settings{Host: "", FromAddress: "x"}
	if bad.Validate() == nil {
		t.Fatal("expected error")
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"535 5.7.8 Username and Password not accepted": KindAuth,
		"x509: certificate signed by unknown authority": KindTLS,
		"550 5.1.1 mailbox unavailable":                 KindRecipient,
		"dial tcp: lookup smtp.nowhere.invalid: no such host": KindNetwork,
		"421 4.7.0 Try again later":                     KindTemporary,
	}
	for msg, kind := range cases {
		var me *Error
		if !errors.As(Classify(errors.New(msg)), &me) || me.Kind != kind {
			t.Errorf("%q => %+v, want %s", msg, me, kind)
		}
	}
}

// fakeSMTP is a minimal plaintext SMTP server that accepts one message.
func fakeSMTP(t *testing.T, failAuth bool) (int, chan string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	go func() {
		defer l.Close()
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { c.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					got <- data.String()
					w("250 OK queued")
					continue
				}
				data.WriteString(line + "\n")
				continue
			}
			up := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
				w("250-fake")
				w("250 AUTH PLAIN LOGIN")
			case strings.HasPrefix(up, "AUTH"):
				if failAuth {
					w("535 5.7.8 Authentication failed")
				} else {
					w("235 OK")
				}
			case strings.HasPrefix(up, "MAIL FROM"), strings.HasPrefix(up, "RCPT TO"), strings.HasPrefix(up, "RSET"), strings.HasPrefix(up, "NOOP"):
				w("250 OK")
			case strings.HasPrefix(up, "DATA"):
				inData = true
				w("354 go ahead")
			case strings.HasPrefix(up, "QUIT"):
				w("221 bye")
				return
			default:
				w("250 OK")
			}
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, got
}

func TestSendAgainstFakeServer(t *testing.T) {
	port, got := fakeSMTP(t, false)
	s := Settings{Host: "127.0.0.1", Port: port, Security: SecNone, AllowInsecureAuth: true, FromAddress: "bot@example.com", Password: "pw"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := Send(ctx, s, Email{To: []string{"x@example.com"}, Subject: "مرحبا", Text: "hello", HTML: "<p>hello</p>"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-got:
		if !strings.Contains(body, "hello") {
			t.Fatalf("body missing text: %s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message received")
	}
}

func TestAuthFailureClassified(t *testing.T) {
	port, _ := fakeSMTP(t, true)
	s := Settings{Host: "127.0.0.1", Port: port, Security: SecNone, AllowInsecureAuth: true, FromAddress: "bot@example.com", Password: "bad"}
	err := Test(context.Background(), s)
	var me *Error
	if !errors.As(err, &me) || me.Kind != KindAuth || me.Transient() {
		t.Fatalf("want auth error, got %v", err)
	}
}
