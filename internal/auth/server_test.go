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
	loginPage := request(t, s, http.MethodGet, "/login?binding=app-public", central, nil, nil, "")
	csrf := findCookie(t, loginPage, csrfCookie)
	if !csrf.Secure || !csrf.HttpOnly || csrf.Path != "/" || csrf.Domain != "" {
		t.Fatalf("invalid CSRF cookie: %#v", csrf)
	}
	if !strings.Contains(loginPage.Body.String(), `name="csrf"`) {
		t.Fatal("login page is missing CSRF field")
	}
	forged := request(t, s, http.MethodPost, "/login", central,
		url.Values{"binding": {"app-public"}, "password": {"correct horse battery staple"}, "csrf": {csrf.Value}}, nil, "https://evil.test")
	if forged.Code != http.StatusForbidden || !strings.Contains(forged.Body.String(), "页面已过期") || !strings.Contains(forged.Body.String(), `type="password"`) {
		t.Fatalf("forged login = %d: %s", forged.Code, forged.Body.String())
	}
	wrongPassword := request(t, s, http.MethodPost, "/login", central,
		url.Values{"binding": {"app-public"}, "password": {"wrong password"}, "csrf": {csrf.Value}}, []*http.Cookie{csrf}, "")
	if wrongPassword.Code != http.StatusOK || !strings.Contains(wrongPassword.Body.String(), "密码不正确，请重试") || !strings.Contains(wrongPassword.Body.String(), `type="password"`) {
		t.Fatalf("wrong password page = %d: %s", wrongPassword.Code, wrongPassword.Body.String())
	}
	login := request(t, s, http.MethodPost, "/login", central,
		url.Values{"binding": {"app-public"}, "password": {"correct horse battery staple"}, "csrf": {csrf.Value}}, []*http.Cookie{csrf}, "")
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != "/duration" {
		t.Fatalf("login response = %d, %s", login.Code, login.Header().Get("Location"))
	}
	stage := findCookie(t, login, stageCookie)
	durationPage := request(t, s, http.MethodGet, "/duration", central, nil, []*http.Cookie{stage, csrf}, "")
	if durationPage.Code != http.StatusOK || !strings.Contains(durationPage.Body.String(), `name="csrf"`) {
		t.Fatalf("duration page = %d", durationPage.Code)
	}
	invalidDuration := request(t, s, http.MethodPost, "/duration", central,
		url.Values{"duration": {"2h"}}, []*http.Cookie{stage, csrf}, "")
	if invalidDuration.Code != http.StatusForbidden || !strings.Contains(invalidDuration.Body.String(), "页面已过期") {
		t.Fatalf("invalid duration = %d: %s", invalidDuration.Code, invalidDuration.Body.String())
	}
	duration := request(t, s, http.MethodPost, "/duration", central,
		url.Values{"duration": {"2h"}, "session_only": {"1"}, "csrf": {csrf.Value}}, []*http.Cookie{stage, csrf}, "")
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
	page := request(t, restarted, http.MethodGet, "/logout", central, nil, []*http.Cookie{browser, csrf}, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "app.example.test") {
		t.Fatalf("logout page = %d: %s", page.Code, page.Body.String())
	}
	invalidLogout := request(t, restarted, http.MethodPost, "/logout", central,
		url.Values{}, []*http.Cookie{browser, csrf}, "")
	if invalidLogout.Code != http.StatusForbidden || !strings.Contains(invalidLogout.Body.String(), "页面已过期") {
		t.Fatalf("invalid logout = %d: %s", invalidLogout.Code, invalidLogout.Body.String())
	}
	logout := request(t, restarted, http.MethodPost, "/logout", central,
		url.Values{"csrf": {csrf.Value}}, []*http.Cookie{browser, csrf}, "")
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
	loginPage := request(t, server, http.MethodGet, "/login?binding=second", central, nil, nil, "")
	csrf := findCookie(t, loginPage, csrfCookie)
	wrong := request(t, server, http.MethodPost, "/login", central,
		url.Values{"binding": {"second"}, "password": {"first site secret passphrase"}, "csrf": {csrf.Value}}, []*http.Cookie{csrf}, "")
	if wrong.Code != http.StatusOK || strings.Contains(wrong.Header().Get("Set-Cookie"), stageCookie) || !strings.Contains(wrong.Body.String(), "密码不正确") {
		t.Fatalf("other site's password accepted: %d %#v", wrong.Code, wrong.Header())
	}
	var browser *http.Cookie
	siteCookies := map[string]*http.Cookie{}
	for _, binding := range []struct{ name, hostname, password string }{
		{"first", "first.example.test", "first site secret passphrase"},
		{"second", "second.example.test", "second site secret passphrase"},
	} {
		cookies := []*http.Cookie{csrf}
		if browser != nil {
			cookies = append(cookies, browser)
		}
		login := request(t, server, http.MethodPost, "/login", central,
			url.Values{"binding": {binding.name}, "password": {binding.password}, "csrf": {csrf.Value}}, cookies, "")
		if login.Code != http.StatusSeeOther {
			t.Fatalf("%s login = %d", binding.name, login.Code)
		}
		duration := request(t, server, http.MethodPost, "/duration", central,
			url.Values{"duration": {"1d"}, "csrf": {csrf.Value}}, []*http.Cookie{findCookie(t, login, stageCookie), csrf}, "")
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
		url.Values{"binding": {"first"}, "csrf": {csrf.Value}}, []*http.Cookie{browser, csrf}, "")
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
