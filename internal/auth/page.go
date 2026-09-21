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
	_, _ = fmt.Fprint(w, `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="theme-color" content="#f6f8ff"><title>`, html.EscapeString(title), ` · Teclab Portal</title><style>
:root{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Display","Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;color:#1b2233;color-scheme:light}
*{box-sizing:border-box}body{min-height:100vh;margin:0;padding:40px 20px;display:flex;background:radial-gradient(ellipse 55% 64% at 7% 12%,#dcecff 0%,transparent 78%),radial-gradient(ellipse 57% 62% at 90% 88%,#ffe3f1 0%,transparent 77%),#fafbff}
button,input{font:inherit}button{cursor:pointer}button:focus-visible,input:focus-visible{outline:3px solid #98b9ff;outline-offset:3px}
.card{width:100%;max-width:640px;min-width:0;margin:auto;padding:48px 54px 38px;background:rgba(255,255,255,.82);border:1px solid rgba(220,225,238,.86);border-radius:28px;box-shadow:0 28px 90px rgba(64,73,112,.10),0 2px 8px rgba(64,73,112,.04);backdrop-filter:blur(22px);-webkit-backdrop-filter:blur(22px)}
.brand{display:flex;align-items:center;justify-content:center;gap:16px;margin:6px 0 40px}.brand-icon{width:70px;height:70px;flex:none;display:grid;place-items:center;border-radius:20px;background:linear-gradient(145deg,#e4f3ff,#f6e7ff);border:1px solid rgba(130,153,214,.34);box-shadow:0 10px 22px rgba(89,118,195,.12)}.brand-icon svg{width:42px;height:42px;fill:none;stroke:#2775d5;stroke-width:2.6;stroke-linecap:round;stroke-linejoin:round}.brand-name{font-size:clamp(30px,7vw,45px);font-weight:750;letter-spacing:-.055em;line-height:1;white-space:nowrap}.brand-name span{background:linear-gradient(105deg,#bd28ee,#d84cd7);background-clip:text;-webkit-background-clip:text;color:transparent}
.intro{text-align:center;margin-bottom:30px}.eyebrow{margin:0 0 10px;color:#7d88a2;font-size:11px;font-weight:750;letter-spacing:.2em}.intro h1{font-size:27px;letter-spacing:-.035em;margin:0 0 10px}.intro p{margin:0;color:#69748b;font-size:14px;line-height:1.7}.host{display:inline-flex;max-width:100%;margin-top:18px;padding:8px 14px;border:1px solid #e5eaf4;border-radius:999px;background:#f6f8fc;color:#53627d;font-size:13px;font-weight:600;overflow-wrap:anywhere;word-break:break-word}
form{margin:0}.field{display:block;padding:13px 18px 12px;border:1px solid #e3e6ef;border-radius:18px;background:#f3f4f8;box-shadow:0 8px 18px rgba(41,50,81,.05);transition:border-color .2s,box-shadow .2s,background .2s}.field:focus-within{background:#fff;border-color:#75a5f4;box-shadow:0 0 0 4px rgba(65,125,230,.12)}.field label{display:block;margin:0 0 5px;color:#626b7e;font-size:13px;font-weight:650}.field input{display:block;width:100%;padding:3px 0;border:0;outline:0;background:transparent;color:#202b42;font-size:19px;line-height:1.5}.field input::placeholder{color:#9299ab}.field input:focus-visible{outline:0}
.notice{display:flex;align-items:flex-start;gap:10px;width:100%;margin:14px 0 0;padding:12px 14px;border:1px solid #f4c9d0;border-radius:14px;background:#fff1f3;color:#a63345;font-size:13px;line-height:1.55;overflow-wrap:anywhere;text-align:left}.notice-icon{display:grid;place-items:center;flex:none;width:19px;height:19px;margin-top:1px;border-radius:50%;background:#d95168;color:#fff;font-size:12px;font-weight:800}
.button{display:inline-flex;align-items:center;justify-content:center;width:100%;min-height:56px;padding:12px 20px;border:0;border-radius:999px;background:#0869e9;color:#fff;font-size:16px;font-weight:700;letter-spacing:.02em;text-decoration:none;box-shadow:0 12px 22px rgba(10,100,228,.22);transition:background .2s,transform .2s,box-shadow .2s}.button:hover{background:#075dce;transform:translateY(-1px);box-shadow:0 15px 25px rgba(10,100,228,.25)}.button:active{transform:translateY(0)}.primary{margin-top:28px}.secondary{width:auto;min-height:40px;padding:9px 17px;background:#eef4ff;color:#1f64c6;font-size:13px;box-shadow:none}.secondary:hover{background:#e0ecff;box-shadow:none}
.hint{display:flex;align-items:flex-start;justify-content:center;gap:8px;margin:18px 0 0;color:#7b8599;font-size:12px;line-height:1.55;text-align:center}.hint svg{width:15px;height:15px;flex:none;margin-top:2px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}.footer{margin-top:34px;padding-top:22px;border-top:1px solid #edf0f6;color:#99a2b5;font-size:11px;letter-spacing:.07em;text-align:center}
.options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin:0}.option{position:relative;display:block;cursor:pointer}.option input{position:absolute;width:1px;height:1px;opacity:0}.option span{display:flex;align-items:center;justify-content:space-between;min-height:53px;padding:12px 16px;border:1px solid #e2e7f0;border-radius:14px;background:#f8f9fc;color:#46536c;font-size:14px;font-weight:650;transition:border-color .2s,background .2s,box-shadow .2s}.option span::after{content:"";width:17px;height:17px;flex:none;border:2px solid #bcc7da;border-radius:50%}.option input:checked+span{border-color:#75a9f7;background:#edf5ff;color:#145cb7;box-shadow:0 0 0 2px rgba(31,110,227,.08)}.option input:checked+span::after{border:5px solid #1472e6}.option input:focus-visible+span{outline:3px solid #98b9ff;outline-offset:2px}.option:last-child{grid-column:1/-1}
.session-choice{display:flex;align-items:flex-start;gap:12px;margin-top:18px;padding:15px 16px;border:1px solid #e6eaf2;border-radius:15px;background:#fbfcff;cursor:pointer}.session-choice input{width:17px;height:17px;flex:none;margin:1px 0 0;accent-color:#0869e9}.session-choice strong{display:block;margin-bottom:3px;color:#35435d;font-size:13px}.session-choice small{display:block;color:#7b879b;font-size:12px;line-height:1.55}.fine-print{margin:18px 0 0;color:#9099aa;font-size:11px;line-height:1.6;text-align:center}
.empty{padding:20px;border:1px solid #e8edf5;border-radius:15px;background:#f7f9fd;color:#68768d;font-size:13px;text-align:center}.sessions{display:grid;gap:10px}.session{padding:16px;border:1px solid #e7ebf4;border-radius:16px;background:#fbfcff}.session strong{display:block;color:#2d3a51;font-size:14px;overflow-wrap:anywhere}.session small{display:block;margin:5px 0 12px;color:#8390a4;font-size:12px;line-height:1.5;overflow-wrap:anywhere}
@media(max-width:560px){body{padding:20px 14px}.card{padding:34px 22px 28px;border-radius:24px}.brand{gap:12px;margin:4px 0 31px}.brand-icon{width:56px;height:56px;border-radius:17px}.brand-icon svg{width:35px;height:35px}.intro{margin-bottom:25px}.intro h1{font-size:24px}.options{gap:8px}.option span{padding:11px 12px;font-size:13px}.footer{margin-top:28px}}
@media(prefers-reduced-motion:reduce){*,*::before,*::after{scroll-behavior:auto!important;transition:none!important}}
</style></head><body><main class="card"><div class="brand"><div class="brand-icon" aria-hidden="true"><svg viewBox="0 0 48 48"><path d="M18 12h12M21 8h6M17 36h14M14 39h20M15 16c-2 3-3 7-3 13v2c0 3 3 5 6 5h12c3 0 6-2 6-5v-2c0-6-1-10-3-13-2-3-6-4-9-4s-7 1-9 4Z"/><path d="M20 22c1-2 3-3 4-3s3 1 4 3c0 3-2 5-4 7-2-2-4-4-4-7Z"/></svg></div><div class="brand-name">Teclab <span>Portal</span></div></div>`, content, `<div class="footer">TECLAB PORTAL · SECURE ACCESS</div></main></body></html>`)
}

func pageIntro(heading, description, hostname string) string {
	var b strings.Builder
	b.WriteString(`<div class="intro"><div class="eyebrow">PRIVATE ACCESS</div><h1>`)
	b.WriteString(html.EscapeString(heading))
	b.WriteString(`</h1><p>`)
	b.WriteString(html.EscapeString(description))
	b.WriteString(`</p>`)
	if hostname != "" {
		fmt.Fprintf(&b, `<div class="host">%s</div>`, html.EscapeString(hostname))
	}
	b.WriteString(`</div>`)
	return b.String()
}

func errorNotice(message string) string {
	if message == "" {
		return ""
	}
	return `<p class="notice" id="form-error" role="alert"><span class="notice-icon" aria-hidden="true">!</span><span>` + html.EscapeString(message) + `</span></p>`
}

func loginPage(binding model.Binding, message, csrfToken string) string {
	var b strings.Builder
	b.WriteString(pageIntro("验证身份", "输入访问密码，继续前往此网站。", binding.Hostname))
	fmt.Fprintf(&b, `<form method="post" action="/login"><input type="hidden" name="binding" value="%s">%s`, html.EscapeString(binding.Name), csrfField(csrfToken))
	b.WriteString(`<div class="field"><label for="password">访问密码</label><input id="password" name="password" type="password" placeholder="请输入密码" autocomplete="current-password" required autofocus maxlength="1024"`)
	if message != "" {
		b.WriteString(` aria-invalid="true" aria-describedby="form-error"`)
	}
	b.WriteString(`></div>`)
	b.WriteString(errorNotice(message))
	b.WriteString(`<button class="button primary" type="submit">继续访问</button></form>`)
	b.WriteString(`<p class="hint"><svg viewBox="0 0 20 20" aria-hidden="true"><rect x="4" y="8" width="12" height="10" rx="2"/><path d="M7 8V6a3 3 0 0 1 6 0v2"/></svg><span>密码仅用于验证此网站的访问权限</span></p>`)
	return b.String()
}

func durationPage(binding model.Binding, csrfToken, message string) string {
	var b strings.Builder
	b.WriteString(pageIntro("选择有效期", "密码已验证。设置本次访问凭据的有效时间。", binding.Hostname))
	b.WriteString(`<form method="post" action="/duration">`)
	b.WriteString(csrfField(csrfToken))
	b.WriteString(`<div class="options" role="group" aria-label="凭据有效期">`)
	for _, option := range []struct{ value, label string }{
		{"2h", "2 小时"}, {"12h", "12 小时"}, {"1d", "1 天"}, {"7d", "1 周"},
		{"30d", "1 个月"}, {"90d", "3 个月"}, {"forever", "永久"},
	} {
		checked := ""
		if option.value == "12h" {
			checked = " checked"
		}
		fmt.Fprintf(&b, `<label class="option"><input type="radio" name="duration" value="%s"%s><span>%s</span></label>`, option.value, checked, option.label)
	}
	b.WriteString(`</div><label class="session-choice"><input type="checkbox" name="session_only" value="1"><span><strong>仅在本次浏览器会话保存</strong><small>关闭浏览器后需重新登录；服务端仍按所选时间失效。</small></span></label>`)
	b.WriteString(errorNotice(message))
	b.WriteString(`<button class="button primary" type="submit">进入网站</button></form>`)
	b.WriteString(`<p class="fine-print">“永久”凭据可随时在退出页面主动撤销。浏览器也可能清理长期 Cookie。</p>`)
	return b.String()
}

func logoutPage(sessions []sessionView, done bool, csrfToken, message string) string {
	var b strings.Builder
	if done {
		b.WriteString(pageIntro("已退出", "选中的访问凭据已在服务端失效。", ""))
	} else {
		b.WriteString(pageIntro("管理登录", "查看本浏览器的访问记录，或主动退出网站。", ""))
	}
	b.WriteString(errorNotice(message))
	if len(sessions) == 0 {
		b.WriteString(`<div class="empty">当前没有有效的受保护网站会话。</div>`)
		return b.String()
	}
	b.WriteString(`<div class="sessions">`)
	for _, value := range sessions {
		fmt.Fprintf(&b, `<div class="session"><strong>%s</strong><small>%s · 有效至 %s</small><form method="post" action="/logout"><input type="hidden" name="binding" value="%s">%s<button class="button secondary" type="submit">退出此网站</button></form></div>`,
			html.EscapeString(value.Hostname), html.EscapeString(value.URL), html.EscapeString(value.Expires), html.EscapeString(value.Binding), csrfField(csrfToken))
	}
	b.WriteString(`</div>`)
	fmt.Fprintf(&b, `<form method="post" action="/logout">%s<button class="button primary" type="submit">退出所有网站</button></form>`, csrfField(csrfToken))
	return b.String()
}

func csrfField(token string) string {
	return `<input type="hidden" name="csrf" value="` + html.EscapeString(token) + `">`
}
