package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aircwo-systems/tarn/internal/awsconnection"
	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/internal/secrets"
)

const ownerCookie = "tarn_aws_owner"

type ownerSession struct {
	csrf, origin string
	expires      time.Time
}

// OwnerControl is a private, process-lifetime capability used only by the CLI.
type OwnerControl struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

func OwnerControlPath(dataDir string) string {
	return filepath.Join(dataDir, "aws-connections", "owner.json")
}

type AWSConnections struct {
	mu               sync.Mutex
	service          *awsconnection.Service
	reason           string
	origins          map[string]bool
	dashboard        string
	control          OwnerControl
	controlPath      string
	bootstrap        string
	bootstrapOrigin  string
	bootstrapExpires time.Time
	sessions         map[string]ownerSession
}

func loopbackHost(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

// NewAWSConnections fails closed independently of the emulated resource APIs.
func NewAWSConnections(cfg *config.Config, vault *secrets.Vault, validator awsconnection.Validator) *AWSConnections {
	a := &AWSConnections{origins: map[string]bool{}, sessions: map[string]ownerSession{}}
	if !loopbackHost(cfg.Host) {
		a.reason = "AWS connections require a loopback server. Restart Tarn with --host 127.0.0.1."
		return a
	}
	base := "http://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	a.origins[base] = true
	a.dashboard = base
	if cfg.AWSUIOrigin != "" {
		u, err := url.Parse(cfg.AWSUIOrigin)
		if err != nil || u.Scheme != "http" || !loopbackHost(u.Hostname()) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			a.reason = "TARN_AWS_UI_ORIGIN must be an explicit loopback HTTP origin."
			return a
		}
		a.dashboard = u.Scheme + "://" + u.Host
		a.origins[a.dashboard] = true
	}
	svc, err := awsconnection.Open(cfg.DataDir, vault, validator)
	if err != nil {
		a.reason = err.Error()
		return a
	}
	a.control = OwnerControl{Endpoint: base, Token: randomCapability()}
	a.controlPath = OwnerControlPath(cfg.DataDir)
	if err := os.MkdirAll(filepath.Dir(a.controlPath), 0700); err != nil {
		a.reason = "Cannot create the private AWS owner directory."
		return a
	}
	if err := os.Chmod(filepath.Dir(a.controlPath), 0700); err != nil {
		a.reason = "Cannot protect the AWS owner directory."
		return a
	}
	data, _ := json.Marshal(a.control)
	// Replace rather than truncate a prior process's file, preserving private mode.
	f, err := os.CreateTemp(filepath.Dir(a.controlPath), ".owner-*")
	if err != nil {
		a.reason = "Cannot create the private AWS owner capability."
		return a
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil || os.Rename(tmp, a.controlPath) != nil {
		a.reason = "Cannot save the private AWS owner capability."
		return a
	}
	a.service = svc
	return a
}

func randomCapability() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("cannot generate an owner capability")
	}
	return hex.EncodeToString(b)
}

func (a *AWSConnections) Close() {
	// Do not remove a replacement server's capability on shutdown.
	data, err := os.ReadFile(a.controlPath)
	var current OwnerControl
	if err == nil && json.Unmarshal(data, &current) == nil && current.Token == a.control.Token {
		_ = os.Remove(a.controlPath)
	}
}

func (a *AWSConnections) Availability() (bool, string) { return a.service != nil, a.reason }

func isAWSConnectionPath(path string) bool {
	return strings.HasPrefix(path, "/_tarn/owner/") || path == "/_tarn/admin/aws-connections" || strings.HasPrefix(path, "/_tarn/admin/aws-connections/")
}

func awsJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func awsError(w http.ResponseWriter, status int, message string) {
	awsJSON(w, status, map[string]string{"error": message})
}

func readAWSJSON(w http.ResponseWriter, r *http.Request, body any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		awsError(w, 415, "Use application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(body) != nil || d.Decode(new(any)) != io.EOF {
		awsError(w, 400, "Invalid connection request.")
		return false
	}
	return true
}

func (a *AWSConnections) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if a == nil || a.service == nil {
		reason := "AWS connection management is unavailable."
		if a != nil && a.reason != "" {
			reason = a.reason
		}
		if r.URL.Path == "/_tarn/owner/status" && r.Method == "GET" {
			awsJSON(w, 200, map[string]any{"available": false, "authenticated": false, "reason": reason})
			return
		}
		awsError(w, 503, reason)
		return
	}
	// Validate both the socket peer and Host. Never trust forwarding headers.
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() {
		awsError(w, 403, "AWS connections are local only.")
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	origin := scheme + "://" + r.Host
	if !a.origins[origin] || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		awsError(w, 403, "Use the configured local dashboard origin.")
		return
	}
	if r.URL.Path == "/_tarn/owner/bootstrap" && r.Method == "POST" {
		if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.control.Token)) != 1 {
			awsError(w, 403, "Run tarn vault unlock from the local terminal.")
			return
		}
		a.mu.Lock()
		a.bootstrap, a.bootstrapOrigin, a.bootstrapExpires = randomCapability(), a.dashboard, time.Now().Add(5*time.Minute)
		link := a.dashboard + "/#settings?aws-unlock=" + a.bootstrap
		a.mu.Unlock()
		awsJSON(w, 200, map[string]string{"url": link})
		return
	}
	if r.URL.Path == "/_tarn/owner/session" && r.Method == "POST" {
		if r.Header.Get("Origin") != origin || r.Header.Get("X-Tarn-Owner-Request") != "1" {
			awsError(w, 403, "Open the unlock link from your local terminal.")
			return
		}
		var body struct {
			Token string `json:"token"`
		}
		if !readAWSJSON(w, r, &body) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if body.Token == "" || subtle.ConstantTimeCompare([]byte(body.Token), []byte(a.bootstrap)) != 1 || time.Now().After(a.bootstrapExpires) || origin != a.bootstrapOrigin {
			awsError(w, 403, "This unlock link has expired or was already used. Run tarn vault unlock again.")
			return
		}
		a.bootstrap = ""
		a.sessions = map[string]ownerSession{} // A new unlock replaces older owner sessions.
		token, csrf := randomCapability(), randomCapability()
		expires := time.Now().Add(8 * time.Hour)
		a.sessions[token] = ownerSession{csrf, origin, expires}
		http.SetCookie(w, &http.Cookie{Name: ownerCookie, Value: token, Path: "/_tarn/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: 8 * 60 * 60})
		awsJSON(w, 200, map[string]any{"available": true, "authenticated": true, "csrfToken": csrf, "manualCredentialsAvailable": a.service.ManualCredentialsAvailable()})
		return
	}
	a.mu.Lock()
	cookie, cookieErr := r.Cookie(ownerCookie)
	var session ownerSession
	if cookieErr == nil {
		session = a.sessions[cookie.Value]
	}
	a.mu.Unlock()
	authenticated := session.origin == origin && time.Now().Before(session.expires)
	if r.URL.Path == "/_tarn/owner/status" && r.Method == "GET" {
		body := map[string]any{"available": true, "authenticated": authenticated}
		if authenticated {
			body["csrfToken"] = session.csrf
			body["manualCredentialsAvailable"] = a.service.ManualCredentialsAvailable()
		}
		awsJSON(w, 200, body)
		return
	}
	if !authenticated {
		awsError(w, 401, "Unlock AWS connections with tarn vault unlock in your terminal.")
		return
	}
	if r.Method != "GET" && (r.Header.Get("Origin") != origin || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Tarn-CSRF")), []byte(session.csrf)) != 1) {
		awsError(w, 403, "The owner session could not verify this request. Reload Settings and try again.")
		return
	}
	if r.URL.Path == "/_tarn/owner/session" && r.Method == "DELETE" {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: ownerCookie, Value: "", Path: "/_tarn/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(204)
		return
	}
	a.connectionRequest(w, r)
}

func (a *AWSConnections) connectionRequest(w http.ResponseWriter, r *http.Request) {
	base := "/_tarn/admin/aws-connections"
	if r.URL.Path == base+"/profiles" && r.Method == "GET" {
		names, err := awsconnection.ProfileNames()
		if err != nil {
			awsError(w, 500, err.Error())
			return
		}
		awsJSON(w, 200, map[string]any{"profiles": names})
		return
	}
	if r.URL.Path == base && r.Method == "GET" {
		awsJSON(w, 200, map[string]any{"connections": a.service.List()})
		return
	}
	var m awsconnection.Metadata
	var err error
	status := 200
	if r.URL.Path == base && r.Method == "POST" {
		var in awsconnection.Input
		if !readAWSJSON(w, r, &in) {
			return
		}
		m, err = a.service.Create(r.Context(), in)
		status = 201
	} else {
		path := strings.TrimPrefix(r.URL.Path, base+"/")
		parts := strings.Split(path, "/")
		id := parts[0]
		switch {
		case len(parts) == 2 && parts[1] == "test" && r.Method == "POST":
			m, err = a.service.Test(r.Context(), id)
		case len(parts) == 1 && r.Method == "PATCH":
			var in awsconnection.Input
			if !readAWSJSON(w, r, &in) {
				return
			}
			m, err = a.service.Update(r.Context(), id, in)
		case len(parts) == 1 && r.Method == "DELETE":
			err = a.service.Delete(id)
			if err == nil {
				w.WriteHeader(204)
				return
			}
		default:
			awsError(w, 404, "Unknown AWS connection operation.")
			return
		}
	}
	if err != nil {
		var changed *awsconnection.IdentityChangedError
		if errors.As(err, &changed) {
			awsJSON(w, 409, map[string]any{"error": changed.Error(), "identity": changed.Identity})
			return
		}
		var validation *awsconnection.ValidationError
		if errors.As(err, &validation) {
			awsJSON(w, 422, validation)
			return
		}
		switch {
		case errors.Is(err, awsconnection.ErrNotFound):
			status = 404
		case errors.Is(err, awsconnection.ErrBusy), errors.Is(err, awsconnection.ErrName):
			status = 409
		case errors.Is(err, awsconnection.ErrStorage):
			status = 500
		default:
			status = 400
		}
		awsError(w, status, err.Error())
		return
	}
	awsJSON(w, status, m)
}
