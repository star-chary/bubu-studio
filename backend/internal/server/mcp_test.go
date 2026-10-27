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
	admin    *authClient
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
	h := NewRouter(nil, &stubAssets{}, db)
	srv.Config.Handler = h
	srv.Start()
	t.Cleanup(srv.Close)
	f := &mcpHarness{db: db, h: h, url: base, admin: adminSignIn(t, h, db)}
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
	w := f.request("GET", "/oauth/authorize?"+v.Encode(), nil, f.admin, nil)
	assertStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "授权读取用户") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("missing consent or frame protection")
	}
	v.Set("csrf_token", f.admin.session.CSRFToken)
	v.Set("decision", "allow")
	w = f.request("POST", "/oauth/authorize", v, f.admin, nil)
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

func TestMCPRemoteDiscoveryAuthorizationAndRealUsers(t *testing.T) {
	f := setupMCP(t)
	user := signIn(t, f.h, "creator@example.test", "regular password")
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
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Name != "list_users" || list.Tools[0].OutputSchema == nil {
		t.Fatalf("tool discovery: %v", err)
	}
	for _, test := range []struct {
		args          map[string]any
		count         int
		total         int64
		limit, offset int
	}{
		{map[string]any{}, 2, 2, 20, 0},
		{map[string]any{"q": "CREATOR", "limit": 1}, 1, 1, 1, 0},
		{map[string]any{"q": user.session.User.ID}, 1, 1, 20, 0},
		{map[string]any{"q": "does-not-exist"}, 0, 0, 20, 0},
		{map[string]any{"limit": 1, "offset": 1}, 1, 2, 1, 1},
		{map[string]any{"offset": 99}, 0, 2, 20, 99},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_users", Arguments: test.args})
		if err != nil || result.IsError {
			t.Fatalf("call failed: %v %+v", err, result)
		}
		b, _ := json.Marshal(result.StructuredContent)
		var output listUsersOutput
		if err := json.Unmarshal(b, &output); err != nil || len(output.Users) != test.count || output.Total != test.total || output.Limit != test.limit || output.Offset != test.offset {
			t.Fatalf("wrong result: %s %v", b, err)
		}
		if strings.Contains(string(b), "password") || strings.Contains(string(b), "token") {
			t.Fatal("private fields leaked")
		}
	}
	for _, args := range []map[string]any{{"limit": 0}, {"limit": 101}, {"limit": 1.5}, {"offset": -1}, {"q": strings.Repeat("a", 255)}, {"role": "admin"}, {"limit": nil}} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_users", Arguments: args})
		if err == nil && !result.IsError {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "grant_credits", Arguments: map[string]any{}})
	if err == nil && !result.IsError {
		t.Fatal("exposed unexpected write tool")
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
	assertStatus(t, f.request("GET", "/oauth/authorize?"+v.Encode(), nil, ordinary, nil), 403)
	v.Set("csrf_token", "wrong")
	v.Set("decision", "allow")
	assertStatus(t, f.request("POST", "/oauth/authorize", v, f.admin, nil), 403)
	v.Set("csrf_token", f.admin.session.CSRFToken)
	assertStatus(t, f.request("POST", "/oauth/authorize", v, f.admin, map[string]string{"Origin": "https://attacker.example"}), 403)
	v.Set("redirect_uri", "https://attacker.example/callback")
	assertStatus(t, f.request("POST", "/oauth/authorize", v, f.admin, nil), 400)
	v = f.authorization(verifier)
	v.Set("code_challenge_method", "plain")
	assertStatus(t, f.request("GET", "/oauth/authorize?"+v.Encode(), nil, f.admin, nil), 400)
	v = f.authorization(verifier)
	v.Set("resource", "https://other.example/mcp")
	assertStatus(t, f.request("GET", "/oauth/authorize?"+v.Encode(), nil, f.admin, nil), 400)
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

func TestMCPRechecksRoleExpiryResourceAndRevocation(t *testing.T) {
	f := setupMCP(t)
	token := f.token(t)
	headers := map[string]string{"Authorization": "Bearer " + token}
	assertStatus(t, f.request("GET", "/oauth/connections", nil, f.admin, nil), 200)
	ctx := context.Background()
	if _, err := f.db.Pool.Exec(ctx, `UPDATE users SET role='user' WHERE id=$1`, f.admin.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 401)
	if _, err := f.db.Pool.Exec(ctx, `UPDATE users SET role='admin',status='disabled' WHERE id=$1`, f.admin.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 401)
	if _, err := f.db.Pool.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, f.admin.session.User.ID); err != nil {
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
	if _, err := f.db.Pool.Exec(ctx, `UPDATE mcp_oauth_tokens SET metadata=jsonb_set(metadata,'{Scope}','"other:read"') WHERE token_hash=$1`, identity.TokenHash(token)); err != nil {
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
	assertStatus(t, f.request("POST", "/oauth/connections", url.Values{"client_id": {f.clientID}, "csrf_token": {f.admin.session.CSRFToken}}, f.admin, nil), 303)
	assertStatus(t, f.request("POST", "/mcp", nil, nil, headers), 401)
	assertStatus(t, f.request("POST", "/oauth/token", f.exchange(code, verifier), nil, nil), 400)
	assertStatus(t, f.request("POST", "/mcp", nil, f.admin, nil), 401) // A browser cookie is not an MCP token.
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

func TestMCPQueryFailureIsAnErrorNotAnEmptyUserList(t *testing.T) {
	f := setupMCP(t)
	session := f.sdkClient(t, f.token(t))
	if _, err := f.db.Pool.Exec(context.Background(), `ALTER TABLE credit_accounts RENAME TO unavailable_credit_accounts`); err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_users", Arguments: map[string]any{}})
	if err != nil || !result.IsError {
		t.Fatalf("expected safe tool error: %v %+v", err, result)
	}
	b, _ := json.Marshal(result)
	if !strings.Contains(string(b), "用户查询暂时不可用") || strings.Contains(string(b), "SQLSTATE") || strings.Contains(string(b), "credit_accounts") {
		t.Fatal("unexpected failure payload")
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
