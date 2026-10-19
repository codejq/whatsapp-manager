// Package gowa integrates the GoWA WhatsApp server: a typed REST client and
// a supervisor that runs the bundled whatsapp.exe as a child process.
package gowa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DeviceID is the fixed GoWA device slot the manager uses (single account in v1).
const DeviceID = "main"

// Errors callers classify on.
var (
	ErrNotLoggedIn = errors.New("whatsapp not logged in")
	ErrUnavailable = errors.New("whatsapp service unavailable")
)

// APIError is a non-2xx GoWA response.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gowa %d %s: %s", e.Status, e.Code, e.Message)
}

// Client calls the GoWA REST API.
type Client struct {
	BaseURL string
	User    string
	Pass    string
	HTTP    *http.Client
}

// NewClient returns a client for http://127.0.0.1:port.
func NewClient(port int, user, pass string) *Client {
	return &Client{
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		User:    user,
		Pass:    pass,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

type envelope struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Results json.RawMessage `json:"results"`
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.User, c.Pass)
	req.Header.Set("X-Device-Id", DeviceID)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) || strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "actively refused") {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	var env envelope
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode >= 300 {
		ae := &APIError{Status: resp.StatusCode, Code: env.Code, Message: env.Message}
		if ae.Message == "" {
			ae.Message = strings.TrimSpace(string(raw))
		}
		if env.Code == "AUTHENTICATION_ERROR" || strings.Contains(strings.ToLower(env.Message), "not logged in") || strings.Contains(strings.ToLower(env.Message), "not connected") {
			return fmt.Errorf("%w: %v", ErrNotLoggedIn, ae)
		}
		return ae
	}
	if out != nil && len(env.Results) > 0 && string(env.Results) != "null" {
		if err := json.Unmarshal(env.Results, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, "", out)
}

func (c *Client) postJSON(ctx context.Context, path string, in, out any) error {
	b, _ := json.Marshal(in)
	return c.do(ctx, http.MethodPost, path, bytes.NewReader(b), "application/json", out)
}

// Health returns nil when the server answers /health.
func (c *Client) Health(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%w: health %d", ErrUnavailable, resp.StatusCode)
	}
	return nil
}

// Version returns the running GoWA version (e.g. "v9.6.0").
func (c *Client) Version(ctx context.Context) (string, error) {
	var r struct {
		Version string `json:"version"`
	}
	err := c.getJSON(ctx, "/app/info", &r)
	return r.Version, err
}

// EnsureDevice creates the "main" device slot if it does not exist.
func (c *Client) EnsureDevice(ctx context.Context) error {
	err := c.postJSON(ctx, "/devices", map[string]string{"device_id": DeviceID}, nil)
	var ae *APIError
	if errors.As(err, &ae) && strings.Contains(ae.Message, "already exists") {
		return nil
	}
	return err
}

// Status is the WhatsApp connection state.
type Status struct {
	IsConnected bool   `json:"is_connected"`
	IsLoggedIn  bool   `json:"is_logged_in"`
	DeviceID    string `json:"device_id"`
	JID         string `json:"jid"`
}

// Status returns the connection state of the main device.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	err := c.getJSON(ctx, "/app/status", &s)
	return s, err
}

// LoginQR starts a QR login. It returns the PNG link and validity in seconds.
func (c *Client) LoginQR(ctx context.Context) (string, int, error) {
	var r struct {
		QRLink     string `json:"qr_link"`
		QRDuration int    `json:"qr_duration"`
	}
	err := c.getJSON(ctx, "/app/login", &r)
	return r.QRLink, r.QRDuration, err
}

// FetchQR downloads the QR PNG. Only links on this GoWA server are allowed.
func (c *Client) FetchQR(ctx context.Context, link string) ([]byte, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(c.BaseURL)
	if u.Host != base.Host || !strings.HasPrefix(u.Path, "/statics/qrcode/") {
		return nil, errors.New("unexpected qr link")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	req.SetBasicAuth(c.User, c.Pass)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("qr fetch %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

// LoginWithCode starts phone-number pairing and returns the 8-character code.
func (c *Client) LoginWithCode(ctx context.Context, phone string) (string, error) {
	var r struct {
		PairCode string `json:"pair_code"`
	}
	err := c.getJSON(ctx, "/app/login-with-code?phone="+url.QueryEscape(phone), &r)
	return r.PairCode, err
}

// Logout unlinks the device from WhatsApp.
func (c *Client) Logout(ctx context.Context) error { return c.getJSON(ctx, "/app/logout", nil) }

// Reconnect asks GoWA to reconnect the socket.
func (c *Client) Reconnect(ctx context.Context) error { return c.getJSON(ctx, "/app/reconnect", nil) }

// Chat is a group or contact.
type Chat struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// Groups lists joined groups (WhatsApp caps this at 500).
func (c *Client) Groups(ctx context.Context) ([]Chat, error) {
	var r struct {
		Data []struct {
			JID  string `json:"JID"`
			Name string `json:"Name"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, "/user/my/groups", &r); err != nil {
		return nil, err
	}
	out := make([]Chat, 0, len(r.Data))
	for _, g := range r.Data {
		out = append(out, Chat{JID: g.JID, Name: g.Name})
	}
	return out, nil
}

// Contacts lists address-book contacts.
func (c *Client) Contacts(ctx context.Context) ([]Chat, error) {
	var r struct {
		Data []Chat `json:"data"`
	}
	if err := c.getJSON(ctx, "/user/my/contacts", &r); err != nil {
		return nil, err
	}
	return r.Data, nil
}

// IsOnWhatsApp checks a phone number.
func (c *Client) IsOnWhatsApp(ctx context.Context, phone string) (bool, error) {
	var r struct {
		IsOnWhatsApp bool `json:"is_on_whatsapp"`
	}
	err := c.getJSON(ctx, "/user/check?phone="+url.QueryEscape(phone), &r)
	return r.IsOnWhatsApp, err
}

type sendResult struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

// Forward natively forwards a stored message to `to` (JID).
func (c *Client) Forward(ctx context.Context, messageID, to string) (string, error) {
	var r sendResult
	err := c.postJSON(ctx, "/message/"+url.PathEscape(messageID)+"/forward", map[string]any{"phone": to}, &r)
	return r.MessageID, err
}

// SendText sends a text message.
func (c *Client) SendText(ctx context.Context, to, text string) (string, error) {
	var r sendResult
	err := c.postJSON(ctx, "/send/message", map[string]any{"phone": to, "message": text}, &r)
	return r.MessageID, err
}

// SendMedia uploads a local file as image/video/audio/file.
func (c *Client) SendMedia(ctx context.Context, kind, to, caption, filePath string) (string, error) {
	endpoint, field := "/send/file", "file"
	switch kind {
	case "image":
		endpoint, field = "/send/image", "image"
	case "video":
		endpoint, field = "/send/video", "video"
	case "audio":
		endpoint, field = "/send/audio", "audio"
	}
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("phone", to)
	if kind != "audio" && caption != "" {
		_ = w.WriteField("caption", caption)
	}
	part, err := w.CreateFormFile(field, filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	w.Close()
	var r sendResult
	err = c.do(ctx, http.MethodPost, endpoint, &buf, w.FormDataContentType(), &r)
	return r.MessageID, err
}

// IsTransient reports whether a send error is worth retrying.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNotLoggedIn) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ae *APIError
	if errors.As(err, &ae) {
		if ae.Status == 400 || ae.Status == 404 || ae.Status == 422 {
			return false
		}
		return true // 5xx: GoWA wraps most WhatsApp-side failures as 500
	}
	return true
}
