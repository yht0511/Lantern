package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lantern/internal/model"
)

const siteCookie = "__Host-lantern_session"
const browserCookie = "__Host-lantern_browser"
const stageCookie = "__Host-lantern_stage"
const maxCookieAge = 400 * 24 * time.Hour

type stage struct {
	binding      string
	browserToken string
	expires      time.Time
}

type ticket struct {
	binding      string
	browserToken string
	siteToken    string
	expires      time.Time
	lifetime     time.Duration
	sessionOnly  bool
}

type attempts struct {
	count int
	until time.Time
}

type Server struct {
	cfg        *model.Config
	secrets    *model.Secrets
	store      *store
	publicURL  *url.URL
	bindings   map[string]model.Binding
	exits      map[string]model.Exit
	mu         sync.Mutex
	stages     map[string]stage
	tickets    map[string]ticket
	attempts   map[string]attempts
	verifyGate chan struct{}
	now        func() time.Time
}

func New(cfg *model.Config, secrets *model.Secrets) (*Server, error) {
	cfg.ApplyDefaults()
	publicURL, err := url.Parse(cfg.Settings.Auth.PublicURL)
	if err != nil || publicURL.Scheme != "https" || publicURL.Host == "" || publicURL.Path != "" || publicURL.RawQuery != "" || publicURL.Fragment != "" || publicURL.User != nil {
		return nil, errors.New("settings.auth.public_url must be an HTTPS origin without a path")
	}
	listenHost, listenPort, err := net.SplitHostPort(cfg.Settings.Auth.Listen)
	port, portErr := strconv.Atoi(listenPort)
	if err != nil || portErr != nil || port < 1 || port > 65535 || net.ParseIP(listenHost) == nil || !net.ParseIP(listenHost).IsLoopback() {
		return nil, errors.New("settings.auth.listen must use a loopback IP and port")
	}
	if !filepath.IsAbs(cfg.Settings.Auth.SessionFile) {
		return nil, errors.New("settings.auth.session_file must be an absolute path")
	}
	if secrets == nil {
		return nil, errors.New("auth secrets are missing")
	}
	sessions, err := loadStore(cfg.Settings.Auth.SessionFile)
	if err != nil {
		return nil, fmt.Errorf("load auth sessions: %w", err)
	}
	s := &Server{cfg: cfg, secrets: secrets, store: sessions, publicURL: publicURL,
		bindings: map[string]model.Binding{}, exits: map[string]model.Exit{}, stages: map[string]stage{},
		tickets: map[string]ticket{}, attempts: map[string]attempts{}, verifyGate: make(chan struct{}, 4), now: time.Now}
	for _, exit := range cfg.Exits {
		s.exits[exit.Name] = exit
	}
	for _, binding := range cfg.Bindings {
		if binding.Disabled || binding.AuthRef == "" {
			continue
		}
		if secrets.AuthPasswords[binding.AuthRef] == "" {
			return nil, fmt.Errorf("missing auth password hash %q for binding %s", binding.AuthRef, binding.Name)
		}
		s.bindings[binding.Name] = binding
	}
	return s, nil
}

func (s *Server) Listen(ctx context.Context) error {
	server := &http.Server{Addr: s.cfg.Settings.Auth.Listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/check", s.check)
	mux.HandleFunc("/consume", s.consume)
	mux.HandleFunc("/login", s.login)
	mux.HandleFunc("/duration", s.duration)
	mux.HandleFunc("/logout", s.logout)
	mux.HandleFunc("/", s.home)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	binding, ok := s.targetBinding(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	cookie, err := r.Cookie(siteCookie)
	if err != nil || !s.store.valid(cookie.Value, binding.Name, digest(s.secrets.AuthPasswords[binding.AuthRef]), s.now()) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) consume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	binding, ok := s.targetBinding(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	key := digest(r.URL.Query().Get("ticket"))
	s.mu.Lock()
	grant, found := s.tickets[key]
	if found {
		delete(s.tickets, key)
	}
	s.mu.Unlock()
	if !found || grant.binding != binding.Name || !s.now().Before(grant.expires) {
		http.Error(w, "login ticket expired; please sign in again", http.StatusUnauthorized)
		return
	}
	now := s.now()
	value := session{ID: digest(grant.siteToken), BrowserID: digest(grant.browserToken), Binding: binding.Name,
		PasswordDigest: digest(s.secrets.AuthPasswords[binding.AuthRef]), CreatedAt: now.Unix()}
	if grant.lifetime != 0 {
		value.ExpiresAt = now.Add(grant.lifetime).Unix()
	}
	if err := s.store.add(value); err != nil {
		http.Error(w, "could not save session", http.StatusInternalServerError)
		return
	}
	setCookie(w, siteCookie, grant.siteToken, cookieAge(grant.lifetime, grant.sessionOnly))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.centralHost(r) {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		binding, ok := s.bindings[r.URL.Query().Get("binding")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		renderPage(w, "登录", loginPage(binding, ""))
	case http.MethodPost:
		if !s.validOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		binding, ok := s.bindings[r.PostForm.Get("binding")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		key := s.clientIP(r) + "|" + binding.Name
		if !s.allowAttempt(key) {
			http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
			return
		}
		select {
		case s.verifyGate <- struct{}{}:
			defer func() { <-s.verifyGate }()
		default:
			http.Error(w, "server busy; try again shortly", http.StatusServiceUnavailable)
			return
		}
		if !VerifyPassword(s.secrets.AuthPasswords[binding.AuthRef], r.PostForm.Get("password")) {
			s.failAttempt(key)
			renderPage(w, "登录", loginPage(binding, "密码不正确，请重试。"))
			return
		}
		s.clearAttempt(key)
		stageToken, err := randomToken()
		if err != nil {
			http.Error(w, "temporary error", http.StatusInternalServerError)
			return
		}
		browserToken := cookieValue(r, browserCookie)
		if !validToken(browserToken) {
			browserToken, err = randomToken()
			if err != nil {
				http.Error(w, "temporary error", http.StatusInternalServerError)
				return
			}
		}
		s.mu.Lock()
		s.stages[digest(stageToken)] = stage{binding: binding.Name, browserToken: browserToken, expires: s.now().Add(5 * time.Minute)}
		s.mu.Unlock()
		setCookie(w, stageCookie, stageToken, 5*time.Minute)
		http.Redirect(w, r, "/duration", http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) duration(w http.ResponseWriter, r *http.Request) {
	if !s.centralHost(r) {
		http.NotFound(w, r)
		return
	}
	stageID := digest(cookieValue(r, stageCookie))
	s.mu.Lock()
	challenge, ok := s.stages[stageID]
	s.mu.Unlock()
	if !ok || !s.now().Before(challenge.expires) {
		http.Error(w, "login step expired; please sign in again", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		renderPage(w, "选择有效期", durationPage(s.bindings[challenge.binding]))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.validOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	lifetime, ok := durations[r.PostForm.Get("duration")]
	if !ok {
		http.Error(w, "invalid duration", http.StatusBadRequest)
		return
	}
	ticketToken, err := randomToken()
	if err != nil {
		http.Error(w, "temporary error", http.StatusInternalServerError)
		return
	}
	siteToken, err := randomToken()
	if err != nil {
		http.Error(w, "temporary error", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	delete(s.stages, stageID)
	s.tickets[digest(ticketToken)] = ticket{binding: challenge.binding, browserToken: challenge.browserToken,
		siteToken: siteToken, expires: s.now().Add(time.Minute), lifetime: lifetime,
		sessionOnly: r.PostForm.Has("session_only")}
	s.mu.Unlock()
	clearCookie(w, stageCookie)
	setCookie(w, browserCookie, challenge.browserToken, maxCookieAge)
	target := s.siteURL(s.bindings[challenge.binding])
	target.Path = "/__lantern/consume"
	target.RawQuery = url.Values{"ticket": {ticketToken}}.Encode()
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.centralHost(r) {
		http.NotFound(w, r)
		return
	}
	browserToken := cookieValue(r, browserCookie)
	if r.Method == http.MethodGet {
		renderPage(w, "退出登录", logoutPage(s.visibleSessions(browserToken), false))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.validOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !validToken(browserToken) {
		http.Error(w, "no browser session", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	binding := r.PostForm.Get("binding")
	if binding != "" {
		if _, ok := s.bindings[binding]; !ok {
			http.Error(w, "invalid binding", http.StatusBadRequest)
			return
		}
	}
	if _, err := s.store.revoke(browserToken, binding); err != nil {
		http.Error(w, "could not revoke sessions", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	for key, value := range s.tickets {
		if value.browserToken == browserToken && (binding == "" || value.binding == binding) {
			delete(s.tickets, key)
		}
	}
	s.mu.Unlock()
	if binding == "" {
		clearCookie(w, browserCookie)
	}
	renderPage(w, "已退出", logoutPage(s.visibleSessions(browserToken), true))
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if !s.centralHost(r) || r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/logout", http.StatusSeeOther)
}

func (s *Server) targetBinding(r *http.Request) (model.Binding, bool) {
	binding, ok := s.bindings[r.Header.Get("X-Lantern-Binding")]
	return binding, ok && strings.EqualFold(requestHostname(r.Host), binding.Hostname)
}

func (s *Server) centralHost(r *http.Request) bool {
	return strings.EqualFold(requestHostname(r.Host), s.publicURL.Hostname())
}

func (s *Server) validOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == s.publicURL.Scheme+"://"+s.publicURL.Host
}

func (s *Server) siteURL(binding model.Binding) *url.URL {
	host := binding.Hostname
	if exit := s.exits[binding.Exit]; exit.Type == "frp" && exit.FRP.RemoteHTTPSPort != 0 && exit.FRP.RemoteHTTPSPort != 443 {
		host = net.JoinHostPort(host, fmt.Sprint(exit.FRP.RemoteHTTPSPort))
	}
	return &url.URL{Scheme: "https", Host: host, Path: "/"}
}

func (s *Server) visibleSessions(browserToken string) []sessionView {
	if !validToken(browserToken) {
		return nil
	}
	var result []sessionView
	for _, value := range s.store.forBrowser(browserToken, s.now()) {
		binding, ok := s.bindings[value.Binding]
		if !ok || value.PasswordDigest != digest(s.secrets.AuthPasswords[binding.AuthRef]) {
			continue
		}
		view := sessionView{Binding: binding.Name, Hostname: binding.Hostname, URL: s.siteURL(binding).String()}
		if value.ExpiresAt == 0 {
			view.Expires = "永久，直到主动退出"
		} else {
			view.Expires = time.Unix(value.ExpiresAt, 0).Local().Format("2006-01-02 15:04")
		}
		result = append(result, view)
	}
	return result
}

func requestHostname(host string) string {
	if value, _, err := net.SplitHostPort(host); err == nil {
		return value
	}
	return host
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validToken(value string) bool {
	data, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(data) == 32
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setCookie(w http.ResponseWriter, name, value string, age time.Duration) {
	cookie := &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
	if age > 0 {
		cookie.MaxAge = int(age.Seconds())
	}
	http.SetCookie(w, cookie)
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", Secure: true, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func cookieAge(lifetime time.Duration, sessionOnly bool) time.Duration {
	if sessionOnly {
		return 0
	}
	if lifetime == 0 || lifetime > maxCookieAge {
		return maxCookieAge
	}
	return lifetime
}

var durations = map[string]time.Duration{
	"2h": 2 * time.Hour, "12h": 12 * time.Hour, "1d": 24 * time.Hour,
	"7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour, "forever": 0,
}

func (s *Server) clientIP(r *http.Request) string {
	if forwarded := net.ParseIP(r.Header.Get("X-Real-IP")); forwarded != nil {
		return forwarded.String()
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) allowAttempt(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.attempts[key]
	return !s.now().Before(value.until) || value.count < 5
}

func (s *Server) failAttempt(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.attempts) >= 10000 {
		for oldKey, value := range s.attempts {
			if !s.now().Before(value.until) {
				delete(s.attempts, oldKey)
			}
		}
		if _, ok := s.attempts[key]; !ok && len(s.attempts) >= 10000 {
			return
		}
	}
	value := s.attempts[key]
	if !s.now().Before(value.until) {
		value = attempts{until: s.now().Add(15 * time.Minute)}
	}
	value.count++
	s.attempts[key] = value
}

func (s *Server) clearAttempt(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, key)
}
