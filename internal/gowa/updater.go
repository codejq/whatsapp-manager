package gowa

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"whatsapp-manager/internal/semver"
)

// DefaultRepo is the GoWA GitHub repository.
const DefaultRepo = "aldinokemal/go-whatsapp-web-multidevice"

// Release is a GoWA release usable on Windows x64.
type Release struct {
	Version      string `json:"version"` // "v9.7.0"
	PageURL      string `json:"page_url"`
	AssetName    string `json:"asset_name"`
	AssetURL     string `json:"-"`
	ChecksumsURL string `json:"-"`
	Size         int64  `json:"size"`
}

// Updater finds, downloads and verifies GoWA releases.
type Updater struct {
	Repo string
	Dir  string // where versions are installed: <Dir>\<version>\whatsapp.exe
	HTTP *http.Client
	API  string // GitHub API base, overridable in tests
}

// NewUpdater creates an updater that installs into dir.
func NewUpdater(dir string) *Updater {
	return &Updater{Repo: DefaultRepo, Dir: dir, HTTP: &http.Client{Timeout: 10 * time.Minute}, API: "https://api.github.com"}
}

// Latest returns the newest published GoWA release with a Windows x64 build.
func (u *Updater) Latest(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.API+"/repos/"+u.Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Release{}, fmt.Errorf("github: %s", resp.Status)
	}
	var rel struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return Release{}, err
	}
	if rel.Draft || rel.Prerelease {
		return Release{}, errors.New("latest release is not stable")
	}
	v := semver.Canonical(rel.TagName)
	if v == "" {
		return Release{}, fmt.Errorf("unexpected tag %q", rel.TagName)
	}
	want := "whatsapp_" + strings.TrimPrefix(v, "v") + "_windows_amd64.zip"
	out := Release{Version: v, PageURL: rel.HTMLURL, AssetName: want}
	for _, a := range rel.Assets {
		switch a.Name {
		case want:
			out.AssetURL, out.Size = a.URL, a.Size
		case "checksums.txt":
			out.ChecksumsURL = a.URL
		}
	}
	if out.AssetURL == "" || out.ChecksumsURL == "" {
		return Release{}, fmt.Errorf("release %s has no Windows build or checksums", v)
	}
	if !strings.HasPrefix(out.AssetURL, "https://") || !strings.HasPrefix(out.ChecksumsURL, "https://") {
		return Release{}, errors.New("insecure download URL")
	}
	return out, nil
}

// ExePath is where a version is (or will be) installed.
func (u *Updater) ExePath(version string) string {
	return filepath.Join(u.Dir, version, "whatsapp.exe")
}

// Install downloads rel, verifies its SHA-256 against the release's
// checksums.txt, and extracts whatsapp.exe. progress receives bytes done/total.
func (u *Updater) Install(ctx context.Context, rel Release, progress func(done, total int64)) (string, error) {
	sums, err := u.checksums(ctx, rel.ChecksumsURL)
	if err != nil {
		return "", fmt.Errorf("checksums: %w", err)
	}
	want, ok := sums[rel.AssetName]
	if !ok {
		return "", fmt.Errorf("no checksum for %s", rel.AssetName)
	}
	dir := filepath.Join(u.Dir, rel.Version)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, rel.AssetName+".part")
	defer os.Remove(zipPath)
	if err := u.download(ctx, rel.AssetURL, zipPath, want, progress); err != nil {
		return "", err
	}
	exe := u.ExePath(rel.Version)
	if err := extractExe(zipPath, exe); err != nil {
		return "", err
	}
	return exe, nil
}

func (u *Updater) checksums(ctx context.Context, url string) (map[string]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	out := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out, sc.Err()
}

func (u *Updater) download(ctx context.Context, url, dst, wantSHA string, progress func(done, total int64)) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: %s", resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength, fn: progress}
	_, err = io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(resp.Body, 500<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, wantSHA)
	}
	return nil
}

type progressWriter struct {
	done, total int64
	fn          func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.fn != nil {
		p.fn(p.done, p.total)
	}
	return len(b), nil
}

// extractExe copies the single .exe from the zip to dst (atomically).
func extractExe(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(f.Name), ".exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		tmp := dst + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 300<<20))
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, dst)
	}
	return errors.New("no .exe in archive")
}

// Prune deletes installed versions except those listed in keep (exe paths).
func (u *Updater) Prune(keep ...string) {
	entries, _ := os.ReadDir(u.Dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(u.Dir, e.Name(), "whatsapp.exe")
		keepIt := false
		for _, k := range keep {
			if strings.EqualFold(filepath.Clean(k), filepath.Clean(p)) {
				keepIt = true
			}
		}
		if !keepIt {
			os.RemoveAll(filepath.Join(u.Dir, e.Name()))
		}
	}
}
