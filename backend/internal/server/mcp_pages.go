package server

import (
	"bytes"
	"html/template"
	"net/url"

	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
)

type oauthPage struct {
	Title, Email, CSRF string
	Client             *persistence.OAuthClient
	Values             url.Values
	Connections        []persistence.OAuthConnection
}

func (g *mcpGateway) renderOAuthPage(c *gin.Context, page oauthPage) {
	var output bytes.Buffer
	if err := mcpPageTemplate.Execute(&output, page); err != nil {
		oauthError(c, 500, "server_error", "页面暂时不可用")
		return
	}
	formAction := "'self'"
	if page.Client != nil {
		// Chromium applies form-action to a POST's redirect too. The callback
		// has already been matched exactly against this client's registration.
		callback, _ := url.Parse(page.Values.Get("redirect_uri"))
		formAction += " " + callback.Scheme + "://" + callback.Host
	}
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action "+formAction+"; frame-ancestors 'none'; base-uri 'none'")
	c.Header("X-Frame-Options", "DENY")
	c.Data(200, "text/html; charset=utf-8", output.Bytes())
}

var mcpPageTemplate = template.Must(template.New("oauth").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} · BuBu</title>
<style>
:root{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;color:#292623;background:#f7f6f2;color-scheme:light}*{box-sizing:border-box}body{margin:0;padding:48px 20px}main{max-width:620px;margin:0 auto;background:#fff;border:1px solid #e4e1da;border-radius:20px;padding:36px}.brand{display:inline-flex;align-items:center;gap:10px;color:inherit;text-decoration:none;font-weight:700}.brand b{display:grid;place-items:center;width:34px;height:34px;border-radius:10px;background:#75614b;color:#fff}h1{font-size:26px;line-height:1.4;margin:34px 0 16px}p{line-height:1.8;overflow-wrap:anywhere}.muted{color:#716a63;font-size:14px}.scope{padding:18px 20px;border-radius:12px;background:#f7f6f2;line-height:1.8;margin:22px 0}.actions{display:flex;gap:12px;margin-top:28px}button{font:inherit;border:1px solid #dcd6cf;border-radius:10px;background:white;padding:12px 18px;cursor:pointer}button.primary{background:#66513f;color:white;border-color:#66513f;flex:1}button:hover{filter:brightness(.95)}a{color:#66513f}button:focus-visible,a:focus-visible{outline:3px solid #9b7b53;outline-offset:3px}.connection{padding:18px 0;border-top:1px solid #e4e1da}.connection form{margin-top:10px}.footer{border-top:1px solid #e4e1da;padding-top:20px;margin-top:30px}@media(max-width:480px){body{padding:20px 12px}main{padding:24px 20px}h1{font-size:23px}.actions{flex-wrap:wrap}}
</style></head><body><main><a class="brand" href="/admin/"><b>B</b>BuBu · 管理台</a><h1>{{.Title}}</h1>
<p class="muted">当前管理员：{{.Email}}</p>
{{if .Client}}
<p><strong>{{.Client.Name}}</strong> 请求读取本平台的用户列表。</p>
<div class="scope"><strong>允许读取</strong><br>用户邮箱、账号 ID、角色、状态、注册时间，以及可用和冻结积分。查询结果将交给你授权的 AI 应用。</div>
<p class="muted">应用名称由接入方提供。请核对回调地址：<br><strong>{{.Values.Get "redirect_uri"}}</strong></p>
<p class="muted">访问授权有效期为 1 小时。你可以在“已授权的 AI 应用”中随时撤销。</p>
<form method="post" action="/oauth/authorize">
<input type="hidden" name="csrf_token" value="{{.CSRF}}">
<input type="hidden" name="client_id" value="{{.Values.Get "client_id"}}">
<input type="hidden" name="redirect_uri" value="{{.Values.Get "redirect_uri"}}">
<input type="hidden" name="response_type" value="code">
<input type="hidden" name="scope" value="users:read">
<input type="hidden" name="resource" value="{{.Values.Get "resource"}}">
<input type="hidden" name="state" value="{{.Values.Get "state"}}">
<input type="hidden" name="code_challenge" value="{{.Values.Get "code_challenge"}}">
<input type="hidden" name="code_challenge_method" value="S256">
<div class="actions"><button name="decision" value="deny">取消</button><button class="primary" name="decision" value="allow">授权读取用户</button></div>
</form>
{{else}}
<p>这里显示当前仍有效的应用授权。撤销后，该应用需要重新获得你的授权才能读取用户。</p>
{{range .Connections}}<section class="connection"><strong>{{.Name}}</strong><p class="muted">有效期至 {{.ExpiresAt.UTC.Format "2006-01-02 15:04:05"}} UTC</p><form method="post" action="/oauth/connections"><input type="hidden" name="csrf_token" value="{{$.CSRF}}"><input type="hidden" name="client_id" value="{{.ClientID}}"><button type="submit">撤销授权</button></form></section>{{else}}<div class="scope">目前没有有效的应用授权。</div>{{end}}
{{end}}
<p class="muted footer"><a href="/oauth/connections">已授权的 AI 应用</a> · <a href="/admin/">返回管理台</a></p>
</main></body></html>`))
