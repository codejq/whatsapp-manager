package gowa

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func fakeGitHub(t *testing.T, tamper bool) (*Updater, []byte) {
	t.Helper()
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("windows-amd64.exe")
	exe := []byte("MZ fake gowa binary")
	w.Write(exe)
	zw.Close()
	sum := sha256.Sum256(zbuf.Bytes())
	sumHex := hex.EncodeToString(sum[:])
	if tamper {
		sumHex = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/x/y/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v9.7.0","html_url":"https://github.com/x/y/releases/v9.7.0","assets":[
				{"name":"whatsapp_9.7.0_windows_amd64.zip","browser_download_url":"%[1]s/zip","size":%[2]d},
				{"name":"whatsapp_9.7.0_linux_amd64.zip","browser_download_url":"%[1]s/other","size":1},
				{"name":"checksums.txt","browser_download_url":"%[1]s/sums","size":10}]}`, srv.URL, zbuf.Len())
		case "/zip":
			w.Write(zbuf.Bytes())
		case "/sums":
			fmt.Fprintf(w, "%s  whatsapp_9.7.0_windows_amd64.zip\nabc  other\n", sumHex)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	u := NewUpdater(t.TempDir())
	u.Repo, u.API, u.HTTP = "x/y", srv.URL, srv.Client()
	return u, exe
}

func TestUpdaterInstall(t *testing.T) {
	u, exe := fakeGitHub(t, false)
	ctx := context.Background()
	rel, err := u.Latest(ctx)
	if err != nil || rel.Version != "v9.7.0" || rel.AssetName != "whatsapp_9.7.0_windows_amd64.zip" {
		t.Fatalf("latest %+v err=%v", rel, err)
	}
	var last int64
	path, err := u.Install(ctx, rel, func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, exe) || path != u.ExePath("v9.7.0") || last == 0 {
		t.Fatalf("installed %q at %s (progress %d)", got, path, last)
	}
	u.Prune() // keeps nothing
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("prune did not remove old version")
	}
}

func TestUpdaterRejectsBadChecksum(t *testing.T) {
	u, _ := fakeGitHub(t, true)
	ctx := context.Background()
	rel, err := u.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Install(ctx, rel, nil); err == nil {
		t.Fatal("tampered download was accepted")
	}
	if _, err := os.Stat(u.ExePath(rel.Version)); !os.IsNotExist(err) {
		t.Fatal("exe written despite checksum mismatch")
	}
}
