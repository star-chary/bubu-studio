package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpHarness struct {
	db       *persistence.Store
	h        http.Handler
	url      string
	user     *authClient
	clientID string
}

func setupMCP(t *testing.T) *mcpHarness {
	t.Helper()
	db := authDB(t)
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String()
	t.Setenv("AUTH_COOKIE_SECURE", "false")
	t.Setenv("APP_ORIGINS", base+",http://127.0.0.1:5173")
	t.Setenv("MCP_PUBLIC_URL", base+"/mcp")
	h := NewRouter(nil, &mcpTestAssets{}, db)
	srv.Config.Handler = h
	srv.Start()
	t.Cleanup(srv.Close)
	f := &mcpHarness{db: db, h: h, url: base, user: signIn(t, h, "creator@example.test", "regular password")}
	w := f.json("/oauth/register", map[string]any{"client_name": "MCP test client", "redirect_uris": []string{"https://client.example.test/callback"}, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code"}})
	assertStatus(t, w, 201)
	var client persistence.OAuthClient
	if json.Unmarshal(w.Body.Bytes(), &client) != nil || client.ID == "" {
		t.Fatal("missing client ID")
	}
	f.clientID = client.ID
	return f
}

func (f *mcpHarness) json(path string, value any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(value)
	r := httptest.NewRequest("POST", f.url+path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func (f *mcpHarness) request(method, path string, values url.Values, client *authClient, headers map[string]string) *httptest.ResponseRecorder {
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	r := httptest.NewRequest(method, f.url+path, body)
	if values != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if client != nil {
		r.AddCookie(client.cookie)
		r.Header.Set("Origin", f.url)
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func (f *mcpHarness) authorization(verifier string) url.Values {
	hash := sha256.Sum256([]byte(verifier))
	return url.Values{"client_id": {f.clientID}, "response_type": {"code"}, "redirect_uri": {"https://client.example.test/callback"}, "scope": {mcpScope}, "resource": {f.url + "/mcp"}, "state": {"test-random-state"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}}
}
func (f *mcpHarness) code(t *testing.T, verifier string) string {
	t.Helper()
	v := f.authorization(verifier)
	w := f.request("GET", "/oauth/authorize?"+v.Encode(), nil, f.user, nil)
	assertStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "授权访问画布") || !strings.Contains(w.Body.String(), "提示词") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("missing consent or frame protection")
	}
	v.Set("csrf_token", f.user.session.CSRFToken)
	v.Set("decision", "allow")
	w = f.request("POST", "/oauth/authorize", v, f.user, nil)
	assertStatus(t, w, 303)
	redirect, err := url.Parse(w.Header().Get("Location"))
	if err != nil || redirect.Host != "client.example.test" || redirect.Query().Get("state") != "test-random-state" || redirect.Query().Get("iss") != f.url {
		t.Fatal("invalid authorization redirect")
	}
	code := redirect.Query().Get("code")
	if !identity.ValidToken(code) {
		t.Fatal("missing authorization code")
	}
	return code
}
func (f *mcpHarness) exchange(code, verifier string) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "client_id": {f.clientID}, "code": {code}, "redirect_uri": {"https://client.example.test/callback"}, "code_verifier": {verifier}, "resource": {f.url + "/mcp"}}
}
func (f *mcpHarness) token(t *testing.T) string {
	t.Helper()
	verifier := identity.Token()
	code := f.code(t, verifier)
	w := f.request("POST", "/oauth/token", f.exchange(code, verifier), nil, nil)
	assertStatus(t, w, 200)
	var token struct {
		Access  string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	if json.Unmarshal(w.Body.Bytes(), &token) != nil || !identity.ValidToken(token.Access) || token.Expires != 3600 {
		t.Fatal("invalid token response")
	}
	if strings.Contains(w.Body.String(), "refresh_token") {
		t.Fatal("unexpected refresh grant")
	}
	return token.Access
}

type bearerTransport string

func (token bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+string(token))
	return http.DefaultTransport.RoundTrip(copy)
}
func (f *mcpHarness) sdkClient(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "acceptance", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.url + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport(token)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestMCPRegistrationAcceptsOptionalRefreshRequestWithoutIssuingRefreshTokens(t *testing.T) {
	f := setupMCP(t)
	metadata := map[string]any{
		"client_name": "Codex", "redirect_uris": []string{"http://127.0.0.1:12345/callback"},
		"token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "scope": mcpScope,
	}
	w := f.json("/oauth/register", metadata)
	assertStatus(t, w, 201)
	var registered struct {
		Grants []string `json:"grant_types"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &registered); err != nil || len(registered.Grants) != 1 || registered.Grants[0] != "authorization_code" {
		t.Fatal("registration must report only the supported authorization code grant")
	}
	metadata["grant_types"] = []string{"refresh_token"}
	assertStatus(t, f.json("/oauth/register", metadata), 400)
	metadata["grant_types"] = []string{"authorization_code", "password"}
	assertStatus(t, f.json("/oauth/register", metadata), 400)
	assertStatus(t, f.request("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}}, nil, nil), 400)
}

func TestMCPRemoteDiscoveryAuthorizationAndCanvasNodes(t *testing.T) {
	f := setupMCP(t)
	other := signIn(t, f.h, "other@example.test", "regular password")
	canvasID, otherCanvasID := identity.ID(), identity.ID()
	if err := f.db.CreateCanvas(context.Background(), canvasID, "MCP test", f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CreateCanvas(context.Background(), otherCanvasID, "other user's canvas", other.session.User.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server"} {
		assertStatus(t, f.request("GET", path, nil, nil, nil), 200)
	}
	w := f.request("POST", "/mcp", nil, nil, nil)
	assertStatus(t, w, 401)
	if !strings.Contains(w.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatal("missing discovery challenge")
	}
	token := f.token(t)
	session := f.sdkClient(t, token)
	list, err := session.ListTools(context.Background(), nil)
	if err != nil || len(list.Tools) != 8 {
		t.Fatalf("tool discovery: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		if tool.OutputSchema == nil {
			t.Fatalf("missing output schema for %s", tool.Name)
		}
		names[tool.Name] = true
	}
	if !names["create_canvas_node"] || !names["list_canvases"] || !names["get_canvas"] || !names["list_generation_models"] || !names["configure_canvas_node"] || !names["quote_generation"] || !names["start_generation"] || !names["get_generation_task"] {
		t.Fatalf("unexpected tools: %v", names)
	}
	nodeID := identity.ID()
	imageArgs := map[string]any{"canvasId": canvasID, "kind": "image", "nodeId": nodeID, "position": map[string]any{"x": 120, "y": 240}}
	for index, args := range []map[string]any{imageArgs, imageArgs, {"canvasId": canvasID, "kind": "video"}} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_canvas_node", Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("call failed: %v %+v", err, result)
		}
		b, _ := json.Marshal(result.StructuredContent)
		var output createCanvasNodeOutput
		if err := json.Unmarshal(b, &output); err != nil || output.CanvasID != canvasID || output.Created != (index != 1) || output.Version != int64(1+index/2) || !persistence.ValidID(output.NodeID) || output.CanvasURL != f.url+"/?canvas="+canvasID {
			t.Fatalf("wrong result: %s %v", b, err)
		}
		if index < 2 && (output.NodeID != nodeID || output.Position.X != 120 || output.Position.Y != 240) {
			t.Fatalf("wrong image node: %s", b)
		}
	}
	canvas, err := f.db.Canvas(context.Background(), canvasID)
	if err != nil || len(canvas.Snapshot.Nodes) != 2 || canvas.Snapshot.Nodes[0].Data.Kind != "image" || canvas.Snapshot.Nodes[1].Data.Kind != "video" || canvas.Snapshot.Nodes[1].Data.VideoMode != "reference" || canvas.Snapshot.Nodes[0].Data.ModelSelection == nil {
		t.Fatalf("nodes were not saved correctly: %+v %v", canvas, err)
	}
	for _, args := range []map[string]any{{}, {"canvasId": canvasID, "kind": "audio"}, {"canvasId": "bad", "kind": "image"}, {"canvasId": canvasID, "kind": "video", "nodeId": nodeID}, {"canvasId": otherCanvasID, "kind": "image"}, {"canvasId": canvasID, "kind": "image", "userId": other.session.User.ID}, {"canvasId": canvasID, "kind": "image", "position": nil}, {"canvasId": canvasID, "kind": "image", "position": map[string]any{"x": 1}}} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_canvas_node", Arguments: args})
		if err == nil && !result.IsError {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_users", Arguments: map[string]any{}})
	if err == nil && !result.IsError {
		t.Fatal("removed admin tool is still callable")
	}
	var tasks, assets int
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM generation_tasks),(SELECT count(*) FROM assets)`).Scan(&tasks, &assets); err != nil || tasks != 0 || assets != 0 {
		t.Fatal("empty node creation unexpectedly created media or a task", err)
	}
	var raw string
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT metadata::text FROM mcp_oauth_tokens WHERE token_hash=$1`, identity.TokenHash(token)).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, token) || strings.Contains(raw, `"Access"`) || strings.Contains(raw, `"Code"`) || strings.Contains(raw, `"Refresh"`) {
		t.Fatal("stored raw credentials")
	}
}

func TestMCPOAuthRejectsForgeryAndAtomicallyConsumesCode(t *testing.T) {
	f := setupMCP(t)
	verifier := identity.Token()
	v := f.authorization(verifier)
	assertStatus(t, f.request("GET", "/oauth/authorize?"+v.Encode(), nil, nil, nil), 303)
	ordinary := signIn(t, f.h, "ordinary@example.test", "regular password")
	assertStatus(t, f.request("GET", "/oauth/authorize?"+v.Encode(), nil, ordinary, nil), 200)
	v.Set("csrf_token", "wrong")
	v.Set("decision", "allow")
	assertStatus(t, f.request("POST", "/oauth/authorize", v, f.user, nil), 403)
	v.Set("csrf_token", f.user.session.CSRFToken)
	assertStatus(t, f.request("POST", "/oauth/authorize", v, f.user, map[string]string{"Origin": "https://attacker.example"}), 403)
	v.Set("redirect_uri", "https://attacker.example/callback")
	assertStatus(t, f.request("POST", "/oauth/authorize", v, f.user, nil), 400)
	v = f.authorization(verifier)
	v.Set("code_challenge_method", "plain")
	assertStatus(t, f.request("GET", "/oauth/authorize?"+v.Encode(), nil, f.user, nil), 400)
	v = f.authorization(verifier)
	v.Set("resource", "https://other.example/mcp")
	assertStatus(t, f.request("GET", "/oauth/authorize?"+v.Encode(), nil, f.user, nil), 400)
	code := f.code(t, verifier)
	v = f.exchange(code, identity.Token())
	assertStatus(t, f.request("POST", "/oauth/token", v, nil, nil), 400)
	v = f.exchange(code, verifier)
	v.Set("redirect_uri", "https://attacker.example")
	assertStatus(t, f.request("POST", "/oauth/token", v, nil, nil), 400)
	v = f.exchange(code, verifier)
	v.Set("client_id", identity.Token())
	assertStatus(t, f.request("POST", "/oauth/token", v, nil, nil), 400)
	v = f.exchange(code, verifier)
	v.Set("resource", "https://other.example/mcp")
	assertStatus(t, f.request("POST", "/oauth/token", v, nil, nil), 400)
	// Failed proofs roll back. Eight correct exchanges race; precisely one commits.
	var success atomic.Int32
	var unexpected atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := f.request("POST", "/oauth/token", f.exchange(code, verifier), nil, nil)
			if w.Code == 200 {
				success.Add(1)
			} else if w.Code != 400 {
				unexpected.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 || unexpected.Load() != 0 {
		t.Fatalf("exchange race: success=%d unexpected=%d", success.Load(), unexpected.Load())
	}
	assertStatus(t, f.request("POST", "/oauth/token", f.exchange(code, verifier), nil, nil), 400)
}

func TestMCPRechecksStatusExpiryResourceAndRevocation(t *testing.T) {
	f := setupMCP(t)
	token := f.token(t)
	headers := map[string]string{"Authorization": "Bearer " + token}
	assertStatus(t, f.request("GET", "/oauth/connections", nil, f.user, nil), 200)
	ctx := context.Background()
	if _, err := f.db.Pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 401)
	if _, err := f.db.Pool.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE mcp_oauth_tokens SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, identity.TokenHash(token)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 401)
	token = f.token(t)
	headers["Authorization"] = "Bearer " + token
	if _, err := f.db.Pool.Exec(ctx, `UPDATE mcp_oauth_tokens SET metadata=jsonb_set(metadata,'{Extension,resource}','["https://wrong.example/mcp"]') WHERE token_hash=$1`, identity.TokenHash(token)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 401)
	token = f.token(t)
	headers["Authorization"] = "Bearer " + token
	if _, err := f.db.Pool.Exec(ctx, `UPDATE mcp_oauth_tokens SET metadata=jsonb_set(metadata,'{Scope}','"users:read"') WHERE token_hash=$1`, identity.TokenHash(token)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 403)
	// A token granted before canvas reading was added cannot gain read access.
	token = f.token(t)
	headers["Authorization"] = "Bearer " + token
	if _, err := f.db.Pool.Exec(ctx, `UPDATE mcp_oauth_tokens SET metadata=jsonb_set(metadata,'{Scope}','"canvases:write"') WHERE token_hash=$1`, identity.TokenHash(token)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 403)
	token = f.token(t)
	headers["Authorization"] = "Bearer " + token
	if _, err := f.db.Pool.Exec(ctx, `UPDATE mcp_oauth_tokens SET metadata=jsonb_set(metadata,'{Scope}','"canvases:read canvases:write"') WHERE token_hash=$1`, identity.TokenHash(token)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 403)
	token = f.token(t)
	headers["Authorization"] = "Bearer " + token
	assertStatus(t, f.request("POST", "/oauth/revoke", url.Values{"token": {token}, "client_id": {f.clientID}}, nil, nil), 200)
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 401)
	token = f.token(t)
	headers["Authorization"] = "Bearer " + token
	verifier := identity.Token()
	code := f.code(t, verifier)
	assertStatus(t, f.request("POST", "/oauth/connections", url.Values{"client_id": {f.clientID}, "csrf_token": {f.user.session.CSRFToken}}, f.user, nil), 303)
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 401)
	assertStatus(t, f.request("POST", "/oauth/token", f.exchange(code, verifier), nil, nil), 400)
	assertStatus(t, f.request("POST", "/mcp", nil, f.user, nil), 401) // A browser cookie is not an MCP token.
}

func TestMCPGuardRejectsHostOriginAndUnboundedRegistration(t *testing.T) {
	f := setupMCP(t)
	r := httptest.NewRequest("POST", "http://attacker.example/mcp", nil)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	assertStatus(t, w, 421)
	assertStatus(t, f.request("POST", "/mcp", nil, nil, map[string]string{"Origin": "https://attacker.example"}), 403)
	assertStatus(t, f.request("GET", "/.well-known/oauth-protected-resource/mcp", nil, nil, map[string]string{"Origin": "https://client.example"}), 200)
	for _, uri := range []string{"https://good.example/callback#fragment", "http://example.com/callback", "javascript:alert(1)", "https://user:pass@example.com/callback"} {
		assertStatus(t, f.json("/oauth/register", map[string]any{"client_name": "bad", "redirect_uris": []string{uri}, "token_endpoint_auth_method": "none"}), 400)
	}
	for range 7 {
		f.json("/oauth/register", map[string]any{})
	}
	assertStatus(t, f.json("/oauth/register", map[string]any{}), 429)
}

func TestMCPCanvasFailureDoesNotExposeDatabaseDetails(t *testing.T) {
	f := setupMCP(t)
	canvasID := identity.ID()
	if err := f.db.CreateCanvas(context.Background(), canvasID, "test", f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	session := f.sdkClient(t, f.token(t))
	if _, err := f.db.Pool.Exec(context.Background(), `ALTER TABLE canvases RENAME TO unavailable_canvases`); err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct {
		name, message string
		args          map[string]any
	}{
		{"create_canvas_node", "节点创建暂时不可用", map[string]any{"canvasId": canvasID, "kind": "image"}},
		{"list_canvases", "画布列表暂时不可用", map[string]any{}},
		{"get_canvas", "画布暂时不可用", map[string]any{"canvasId": canvasID}},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil || !result.IsError {
			t.Fatalf("%s: expected safe tool error: %v %+v", call.name, err, result)
		}
		b, _ := json.Marshal(result)
		if !strings.Contains(string(b), call.message) || strings.Contains(string(b), "SQLSTATE") || strings.Contains(string(b), "unavailable_canvases") {
			t.Fatalf("%s: unexpected failure payload", call.name)
		}
	}
}

func TestMCPConfigurationAndDisabledDefault(t *testing.T) {
	t.Setenv("MCP_PUBLIC_URL", "")
	t.Setenv("AUTH_COOKIE_SECURE", "false")
	t.Setenv("APP_ORIGINS", "http://127.0.0.1:8080")
	h := NewRouter(nil, &stubAssets{}, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1:8080/mcp", nil))
	assertStatus(t, w, 404)
	for _, raw := range []string{"http://example.com/mcp", "http://127.0.0.1:8080/api/mcp", "http://127.0.0.1:8080/mcp?x=y", "http://127.0.0.1:8081/mcp"} {
		t.Setenv("MCP_PUBLIC_URL", raw)
		if _, err := MCPConfigFromEnv(); err == nil {
			t.Fatalf("accepted invalid config %s", raw)
		}
	}
	t.Setenv("MCP_PUBLIC_URL", "http://127.0.0.1:8080/mcp")
	if _, err := MCPConfigFromEnv(); err != nil {
		t.Fatal(err)
	}
}
