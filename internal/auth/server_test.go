package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lantern/internal/model"
)

func TestLoginDurationCheckAndLogout(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil || !VerifyPassword(hash, "correct horse battery staple") || VerifyPassword(hash, "wrong password") {
		t.Fatalf("password hash verification failed: %v", err)
	}
	cfg := &model.Config{
		Settings: model.Settings{Auth: model.AuthSettings{
			PublicURL: "https://auth.example.test:10043", SessionFile: filepath.Join(t.TempDir(), "sessions.json"),
		}},
		Services: []model.Service{{Name: "app", Protocol: "http", Host: "127.0.0.1", Port: 8080}},
		Exits:    []model.Exit{{Name: "public", Type: "frp", FRP: model.FRPExit{RemoteHTTPSPort: 10043}}},
		Bindings: []model.Binding{{Name: "app-public", Service: "app", Exit: "public", Hostname: "app.example.test", SSL: true, AuthRef: "app_password"}},
	}
	secrets := &model.Secrets{AuthPasswords: map[string]string{"app_password": hash}}
	s, err := New(cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}
	central := "auth.example.test:10043"
	target := "app.example.test"
	wrongOrigin := request(t, s, http.MethodPost, "/login", central, url.Values{"binding": {"app-public"}, "password": {"correct horse battery staple"}}, nil, "https://evil.test")
	if wrongOrigin.Code != http.StatusForbidden {
		t.Fatalf("wrong origin = %d", wrongOrigin.Code)
	}
	login := request(t, s, http.MethodPost, "/login", central, url.Values{"binding": {"app-public"}, "password": {"correct horse battery staple"}}, nil, "https://"+central)
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != "/duration" {
		t.Fatalf("login response = %d, %s", login.Code, login.Header().Get("Location"))
	}
	stage := findCookie(t, login, stageCookie)
	duration := request(t, s, http.MethodPost, "/duration", central, url.Values{"duration": {"2h"}, "session_only": {"1"}}, []*http.Cookie{stage}, "https://"+central)
	if duration.Code != http.StatusSeeOther {
		t.Fatalf("duration response = %d: %s", duration.Code, duration.Body.String())
	}
	browser := findCookie(t, duration, browserCookie)
	ticketURL, err := url.Parse(duration.Header().Get("Location"))
	if err != nil || ticketURL.Host != "app.example.test:10043" || !strings.HasPrefix(ticketURL.Path, "/__lantern/consume") {
		t.Fatalf("ticket redirect = %s, %v", ticketURL, err)
	}
	wrongHost := requestWithBinding(t, s, http.MethodGet, "/consume?"+ticketURL.RawQuery, "other.example.test", nil, nil, "", "app-public")
	if wrongHost.Code != http.StatusNotFound {
		t.Fatalf("wrong host consume = %d", wrongHost.Code)
	}
	consume := requestWithBinding(t, s, http.MethodGet, "/consume?"+ticketURL.RawQuery, target, nil, nil, "", "app-public")
	if consume.Code != http.StatusSeeOther {
		t.Fatalf("consume response = %d: %s", consume.Code, consume.Body.String())
	}
	site := findCookie(t, consume, siteCookie)
	if site.MaxAge != 0 || !site.Secure || !site.HttpOnly {
		t.Fatalf("session-only site cookie = %#v", site)
	}
	check := requestWithBinding(t, s, http.MethodGet, "/check", target, nil, []*http.Cookie{site}, "", "app-public")
	if check.Code != http.StatusNoContent {
		t.Fatalf("authorized check = %d", check.Code)
	}
	restarted, err := New(cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}
	check = requestWithBinding(t, restarted, http.MethodGet, "/check", target, nil, []*http.Cookie{site}, "", "app-public")
	if check.Code != http.StatusNoContent {
		t.Fatalf("session after restart = %d", check.Code)
	}
	restarted.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	check = requestWithBinding(t, restarted, http.MethodGet, "/check", target, nil, []*http.Cookie{site}, "", "app-public")
	if check.Code != http.StatusUnauthorized {
		t.Fatalf("expired session check = %d", check.Code)
	}
	restarted.now = time.Now
	page := request(t, restarted, http.MethodGet, "/logout", central, nil, []*http.Cookie{browser}, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "app.example.test") {
		t.Fatalf("logout page = %d: %s", page.Code, page.Body.String())
	}
	logout := request(t, restarted, http.MethodPost, "/logout", central, url.Values{}, []*http.Cookie{browser}, "https://"+central)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout response = %d", logout.Code)
	}
	check = requestWithBinding(t, restarted, http.MethodGet, "/check", target, nil, []*http.Cookie{site}, "", "app-public")
	if check.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session check = %d", check.Code)
	}
}

func TestPasswordsAndLogoutArePerBinding(t *testing.T) {
	firstHash, err := HashPassword("first site secret passphrase")
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := HashPassword("second site secret passphrase")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &model.Config{
		Settings: model.Settings{Auth: model.AuthSettings{
			PublicURL: "https://auth.example.test", SessionFile: filepath.Join(t.TempDir(), "sessions.json"),
		}},
		Services: []model.Service{{Name: "app", Protocol: "http", Host: "127.0.0.1", Port: 8080}},
		Bindings: []model.Binding{
			{Name: "first", Service: "app", Hostname: "first.example.test", SSL: true, AuthRef: "first_password"},
			{Name: "second", Service: "app", Hostname: "second.example.test", SSL: true, AuthRef: "second_password"},
		},
	}
	server, err := New(cfg, &model.Secrets{AuthPasswords: map[string]string{"first_password": firstHash, "second_password": secondHash}})
	if err != nil {
		t.Fatal(err)
	}
	central := "auth.example.test"
	origin := "https://" + central
	wrong := request(t, server, http.MethodPost, "/login", central,
		url.Values{"binding": {"second"}, "password": {"first site secret passphrase"}}, nil, origin)
	if wrong.Code != http.StatusOK || strings.Contains(wrong.Header().Get("Set-Cookie"), stageCookie) {
		t.Fatalf("other site's password accepted: %d %#v", wrong.Code, wrong.Header())
	}
	var browser *http.Cookie
	siteCookies := map[string]*http.Cookie{}
	for _, binding := range []struct{ name, hostname, password string }{
		{"first", "first.example.test", "first site secret passphrase"},
		{"second", "second.example.test", "second site secret passphrase"},
	} {
		var cookies []*http.Cookie
		if browser != nil {
			cookies = []*http.Cookie{browser}
		}
		login := request(t, server, http.MethodPost, "/login", central,
			url.Values{"binding": {binding.name}, "password": {binding.password}}, cookies, origin)
		if login.Code != http.StatusSeeOther {
			t.Fatalf("%s login = %d", binding.name, login.Code)
		}
		duration := request(t, server, http.MethodPost, "/duration", central,
			url.Values{"duration": {"1d"}}, []*http.Cookie{findCookie(t, login, stageCookie)}, origin)
		if duration.Code != http.StatusSeeOther {
			t.Fatalf("%s duration = %d", binding.name, duration.Code)
		}
		browser = findCookie(t, duration, browserCookie)
		ticketURL, err := url.Parse(duration.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		consumed := requestWithBinding(t, server, http.MethodGet, "/consume?"+ticketURL.RawQuery,
			binding.hostname, nil, nil, "", binding.name)
		if consumed.Code != http.StatusSeeOther {
			t.Fatalf("%s consume = %d", binding.name, consumed.Code)
		}
		site := findCookie(t, consumed, siteCookie)
		siteCookies[binding.name] = site
		check := requestWithBinding(t, server, http.MethodGet, "/check", binding.hostname, nil, []*http.Cookie{site}, "", binding.name)
		if check.Code != http.StatusNoContent {
			t.Fatalf("%s check = %d", binding.name, check.Code)
		}
	}
	logout := request(t, server, http.MethodPost, "/logout", central,
		url.Values{"binding": {"first"}}, []*http.Cookie{browser}, origin)
	if logout.Code != http.StatusOK || !strings.Contains(logout.Body.String(), "second.example.test") || strings.Contains(logout.Body.String(), "first.example.test") {
		t.Fatalf("single-site logout = %d: %s", logout.Code, logout.Body.String())
	}
	firstCheck := requestWithBinding(t, server, http.MethodGet, "/check", "first.example.test", nil,
		[]*http.Cookie{siteCookies["first"]}, "", "first")
	secondCheck := requestWithBinding(t, server, http.MethodGet, "/check", "second.example.test", nil,
		[]*http.Cookie{siteCookies["second"]}, "", "second")
	if firstCheck.Code != http.StatusUnauthorized || secondCheck.Code != http.StatusNoContent {
		t.Fatalf("single-site logout checks = %d, %d", firstCheck.Code, secondCheck.Code)
	}
}

func request(t *testing.T, server *Server, method, path, host string, form url.Values, cookies []*http.Cookie, origin string) *httptest.ResponseRecorder {
	t.Helper()
	return requestWithBinding(t, server, method, path, host, form, cookies, origin, "")
}

func requestWithBinding(t *testing.T, server *Server, method, path, host string, form url.Values, cookies []*http.Cookie, origin, binding string) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, "https://"+host+path, body)
	r.Host = host
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if binding != "" {
		r.Header.Set("X-Lantern-Binding", binding)
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	return w
}

func findCookie(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %s missing", name)
	return nil
}
