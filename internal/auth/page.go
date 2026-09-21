package auth

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"lantern/internal/model"
)

type sessionView struct {
	Binding  string
	Hostname string
	URL      string
	Expires  string
}

func renderPageStatus(w http.ResponseWriter, status int, title, content string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s · Lantern</title><style>
:root{color-scheme:light;font-family:-apple-system,BlinkMacSystemFont,"SF Pro Display","Segoe UI",sans-serif;color:#17233b}
*{box-sizing:border-box}body{min-height:100vh;margin:0;display:grid;place-items:center;padding:24px;background:radial-gradient(circle at 14%% 10%%,#dbeafe 0,transparent 32%%),radial-gradient(circle at 86%% 85%%,#e0e7ff 0,transparent 38%%),#f5f7fb}
main{width:min(100%%,460px);background:rgba(255,255,255,.88);border:1px solid rgba(255,255,255,.95);border-radius:28px;padding:34px;box-shadow:0 24px 80px rgba(31,52,91,.13);backdrop-filter:blur(24px)}
.mark{width:52px;height:52px;display:grid;place-items:center;border-radius:16px;background:linear-gradient(145deg,#75b7ff,#426ee9);color:#fff;font-size:26px;font-weight:700;box-shadow:0 10px 24px rgba(59,107,217,.25)}
h1{font-size:26px;letter-spacing:-.04em;margin:24px 0 8px}p{line-height:1.55;color:#69758c;margin:0 0 22px}.host{font-size:14px;color:#56647d;overflow-wrap:anywhere}
label{display:block;font-size:14px;font-weight:600;margin:18px 0 8px}input[type=password]{width:100%%;border:1px solid #d7dfeb;border-radius:14px;padding:15px 16px;font:inherit;background:#fff;outline:none;transition:border .2s,box-shadow .2s}input[type=password]:focus{border-color:#4b83ee;box-shadow:0 0 0 4px #d9e7ff}
button,.button{appearance:none;display:inline-flex;align-items:center;justify-content:center;min-height:46px;border:0;border-radius:13px;padding:0 18px;background:#3575e8;color:#fff;font:600 15px inherit;text-decoration:none;cursor:pointer;box-shadow:0 7px 18px rgba(53,117,232,.2)}button:hover,.button:hover{background:#2767d7}.primary{width:100%%;margin-top:22px}.secondary{background:#eaf0fb;color:#2459b5;box-shadow:none}.secondary:hover{background:#dce7fb}
.options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:9px;margin:18px 0}.option{margin:0;padding:14px;border:1px solid #dce3ee;border-radius:13px;background:#fff;cursor:pointer}.option:has(input:checked){border-color:#4b83ee;background:#eef5ff;box-shadow:0 0 0 2px #dceaff}.option input{accent-color:#3575e8;margin:0 7px 0 0}.check{display:flex;align-items:flex-start;gap:10px;font-weight:400;line-height:1.45;color:#56647d}.check input{margin-top:3px;accent-color:#3575e8}
.notice{padding:12px 14px;background:#fff2f1;border-radius:12px;color:#b23d3d;font-size:14px}.session{padding:14px 0;border-top:1px solid #e8edf4}.session strong{display:block;font-size:15px}.session small{display:block;color:#758197;margin:5px 0 11px;overflow-wrap:anywhere}.session form{margin:0}.session button{min-height:36px;font-size:13px}.footer{font-size:12px;color:#94a0b0;text-align:center;margin-top:24px}.empty{padding:18px;border-radius:14px;background:#f3f6fb;color:#66738a}.actions{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:20px}
</style></head><body><main><div class="mark">✦</div>%s<div class="footer">Lantern · 安全连接</div></main></body></html>`, html.EscapeString(title), content)
}

func loginPage(binding model.Binding, message, csrfToken string) string {
	var b strings.Builder
	b.WriteString("<h1>验证身份</h1><p>输入此网站的访问密码，继续安全访问。</p>")
	fmt.Fprintf(&b, "<div class=\"host\">%s</div>", html.EscapeString(binding.Hostname))
	if message != "" {
		fmt.Fprintf(&b, "<p class=\"notice\" role=\"alert\">%s</p>", html.EscapeString(message))
	}
	fmt.Fprintf(&b, `<form method="post" action="/login"><input type="hidden" name="binding" value="%s">%s<label for="password">访问密码</label><input id="password" name="password" type="password" autocomplete="current-password" required autofocus maxlength="1024"><button class="primary" type="submit">继续</button></form>`, html.EscapeString(binding.Name), csrfField(csrfToken))
	return b.String()
}

func durationPage(binding model.Binding, csrfToken, message string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<h1>选择有效期</h1><p>密码已验证。为 %s 选择本次访问的有效时间。</p>", html.EscapeString(binding.Hostname))
	if message != "" {
		fmt.Fprintf(&b, "<p class=\"notice\" role=\"alert\">%s</p>", html.EscapeString(message))
	}
	b.WriteString(`<form method="post" action="/duration">`)
	b.WriteString(csrfField(csrfToken))
	b.WriteString(`<div class="options">`)
	for _, option := range []struct{ value, label string }{
		{"2h", "2 小时"}, {"12h", "12 小时"}, {"1d", "1 天"}, {"7d", "1 周"},
		{"30d", "1 个月"}, {"90d", "3 个月"}, {"forever", "永久"},
	} {
		checked := ""
		if option.value == "12h" {
			checked = " checked"
		}
		fmt.Fprintf(&b, `<label class="option"><input type="radio" name="duration" value="%s"%s>%s</label>`, option.value, checked, option.label)
	}
	b.WriteString(`</div><label class="check"><input type="checkbox" name="session_only" value="1"><span>只在本次浏览器会话保存此网站的登录 Cookie；关闭浏览器后需要重新登录。服务端仍按上方时间失效。</span></label><button class="primary" type="submit">进入网站</button></form>`)
	b.WriteString(`<p class="footer">“永久”会话在服务端持续有效，浏览器仍可能清理长期 Cookie。你可以随时主动退出。</p>`)
	return b.String()
}

func logoutPage(sessions []sessionView, done bool, csrfToken, message string) string {
	var b strings.Builder
	if done {
		b.WriteString("<h1>已退出</h1><p>选中的访问凭据已在服务端失效。</p>")
	} else {
		b.WriteString("<h1>管理登录</h1><p>在这里退出单个网站，或一次退出本浏览器登录过的所有网站。</p>")
	}
	if message != "" {
		fmt.Fprintf(&b, "<p class=\"notice\" role=\"alert\">%s</p>", html.EscapeString(message))
	}
	if len(sessions) == 0 {
		b.WriteString(`<div class="empty">当前没有有效的受保护网站会话。</div>`)
		return b.String()
	}
	for _, value := range sessions {
		fmt.Fprintf(&b, `<div class="session"><strong>%s</strong><small>%s · 有效至 %s</small><form method="post" action="/logout"><input type="hidden" name="binding" value="%s">%s<button class="secondary" type="submit">退出此网站</button></form></div>`,
			html.EscapeString(value.Hostname), html.EscapeString(value.URL), html.EscapeString(value.Expires), html.EscapeString(value.Binding), csrfField(csrfToken))
	}
	fmt.Fprintf(&b, `<form method="post" action="/logout">%s<button class="primary" type="submit">退出所有网站</button></form>`, csrfField(csrfToken))
	return b.String()
}

func csrfField(token string) string {
	return `<input type="hidden" name="csrf" value="` + html.EscapeString(token) + `">`
}
