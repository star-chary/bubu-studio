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
	c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; form-action "+formAction+"; frame-ancestors 'none'; base-uri 'none'")
	c.Header("X-Frame-Options", "DENY")
	c.Data(200, "text/html; charset=utf-8", output.Bytes())
}

var mcpPageTemplate = template.Must(template.New("oauth").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" type="image/svg+xml" href="/favicon.svg?v=bubu-2"><title>{{.Title}} · Bubu</title>
<style>
:root{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;color:#292623;background:#f7f6f2;color-scheme:light}*{box-sizing:border-box}body{margin:0;padding:48px 20px}main{max-width:620px;margin:0 auto;background:#fff;border:1px solid #e4e1da;border-radius:20px;padding:36px}.brand{display:inline-flex;align-items:center;gap:10px;color:inherit;text-decoration:none;font-weight:700}.brand img{display:block;width:34px;height:34px}h1{font-size:26px;line-height:1.4;margin:34px 0 16px}p{line-height:1.8;overflow-wrap:anywhere}.muted{color:#716a63;font-size:14px}.scope{padding:18px 20px;border-radius:12px;background:#f7f6f2;line-height:1.8;margin:22px 0}.actions{display:flex;gap:12px;margin-top:28px}button{font:inherit;border:1px solid #dcd6cf;border-radius:10px;background:white;padding:12px 18px;cursor:pointer}button.primary{background:#66513f;color:white;border-color:#66513f;flex:1}button:hover{filter:brightness(.95)}a{color:#66513f}button:focus-visible,a:focus-visible{outline:3px solid #9b7b53;outline-offset:3px}.connection{padding:18px 0;border-top:1px solid #e4e1da}.connection form{margin-top:10px}.footer{border-top:1px solid #e4e1da;padding-top:20px;margin-top:30px}@media(max-width:480px){body{padding:20px 12px}main{padding:24px 20px}h1{font-size:23px}.actions{flex-wrap:wrap}}
</style></head><body><main><a class="brand" href="/"><img src="/favicon.svg?v=bubu-2" width="34" height="34" alt="">Bubu</a><h1>{{.Title}}</h1>
<p class="muted">当前账号：{{.Email}}</p>
{{if .Client}}
<p><strong>{{.Client.Name}}</strong> 请求访问你的画布。</p>
<div class="scope"><strong>允许读取</strong><br>查看你拥有的画布列表，以及节点名称、提示词、模型参数、引用关系和素材与任务摘要。<br><br><strong>允许编辑</strong><br>在你拥有的画布中创建图片或视频节点并设置参数。<br><br><strong>允许生成</strong><br>应用可以提交图片或视频生成任务，并使用你的积分。每次任务按当前报价冻结并结算积分。</div>
<p class="muted">应用名称由接入方提供。请核对回调地址：<br><strong>{{.Values.Get "redirect_uri"}}</strong></p>
<p class="muted">访问授权有效期为 1 小时。你可以在“已授权的 AI 应用”中随时撤销。</p>
<form method="post" action="/oauth/authorize">
<input type="hidden" name="csrf_token" value="{{.CSRF}}">
<input type="hidden" name="client_id" value="{{.Values.Get "client_id"}}">
<input type="hidden" name="redirect_uri" value="{{.Values.Get "redirect_uri"}}">
<input type="hidden" name="response_type" value="code">
<input type="hidden" name="scope" value="{{.Values.Get "scope"}}">
<input type="hidden" name="resource" value="{{.Values.Get "resource"}}">
<input type="hidden" name="state" value="{{.Values.Get "state"}}">
<input type="hidden" name="code_challenge" value="{{.Values.Get "code_challenge"}}">
<input type="hidden" name="code_challenge_method" value="S256">
<div class="actions"><button name="decision" value="deny">取消</button><button class="primary" name="decision" value="allow">授权访问画布</button></div>
</form>
{{else}}
<p>这里显示当前仍有效的画布读取、节点编辑和生成授权。撤销后，该应用需要重新获得你的授权才能访问画布。</p>
{{range .Connections}}<section class="connection"><strong>{{.Name}}</strong><p class="muted">有效期至 {{.ExpiresAt.UTC.Format "2006-01-02 15:04:05"}} UTC</p><form method="post" action="/oauth/connections"><input type="hidden" name="csrf_token" value="{{$.CSRF}}"><input type="hidden" name="client_id" value="{{.ClientID}}"><button type="submit">撤销授权</button></form></section>{{else}}<div class="scope">目前没有有效的应用授权。</div>{{end}}
{{end}}
<p class="muted footer"><a href="/oauth/connections">已授权的 AI 应用</a> · <a href="/">返回首页</a></p>
</main></body></html>`))
