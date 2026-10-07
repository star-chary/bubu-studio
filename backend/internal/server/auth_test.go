package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func authDB(t *testing.T) *persistence.Store {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("requires isolated PostgreSQL TEST_DATABASE_URL")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("auth tests require a database ending in _test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal("test connection failed")
	}
	schema := fmt.Sprintf("test_auth_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := persistence.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); admin.Close() })
	return db
}

type authClient struct {
	cookie  *http.Cookie
	session persistence.Session
}

func authRequest(handler http.Handler, client *authClient, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("X-Requested-With", "frame-space")
	if client != nil {
		req.AddCookie(client.cookie)
		req.Header.Set("X-CSRF-Token", client.session.CSRFToken)
		req.Header.Set("X-Session-User", client.session.User.ID)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}
func assertStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("expected %d, got %d: %s", want, w.Code, w.Body.String())
	}
}
func signIn(t *testing.T, h http.Handler, email, password string) *authClient {
	t.Helper()
	w := authRequest(h, nil, "POST", "/api/auth/login", map[string]string{"email": email, "password": password}, nil)
	assertStatus(t, w, 200)
	var session persistence.Session
	if json.Unmarshal(w.Body.Bytes(), &session) != nil {
		t.Fatal("invalid session response")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" || cookies[0].MaxAge < 1 {
		t.Fatal("cookie policy")
	}
	if strings.Contains(w.Body.String(), cookies[0].Value) {
		t.Fatal("session secret leaked in JSON")
	}
	return &authClient{cookies[0], session}
}

func TestAuthPasswordLengthBounds(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &stubAssets{}, db)
	for _, length := range []int{5, 21} {
		email := fmt.Sprintf("invalid-%d@example.test", length)
		w := authRequest(h, nil, "POST", "/api/auth/login", map[string]string{"email": email, "password": strings.Repeat("a", length)}, nil)
		assertStatus(t, w, http.StatusBadRequest)
		if !strings.Contains(w.Body.String(), "INVALID_PASSWORD") {
			t.Fatal("expected password validation error")
		}
		var count int
		if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email=$1`, email).Scan(&count); err != nil || count != 0 {
			t.Fatal("invalid password created an account", err)
		}
	}
	for _, length := range []int{6, 20} {
		email := fmt.Sprintf("valid-%d@example.test", length)
		password := strings.Repeat("a", length)
		registered := signIn(t, h, email, password)
		loggedIn := signIn(t, h, email, password)
		if registered.session.User.ID != loggedIn.session.User.ID {
			t.Fatal("login created a duplicate user")
		}
	}
}

func TestAuthLoginOwnershipAndRevocation(t *testing.T) {
	db := authDB(t)
	ctx := context.Background()
	objects := &stubAssets{}
	var calls atomic.Int32
	h := NewRouter(generateFunc(func(context.Context, string, string) (ark.ImageResult, error) {
		calls.Add(1)
		return ark.ImageResult{}, nil
	}), objects, db)
	password := "my creative password"
	for _, path := range []string{"/api/auth/me", "/api/canvases", "/api/assets/content?key=known", "/api/storage/config", "/api/tasks/" + identity.ID()} {
		assertStatus(t, authRequest(h, nil, "GET", path, nil, nil), 401)
	}
	for _, path := range []string{"/api/images/generations", "/api/videos/generations", "/api/assets", "/api/canvases"} {
		assertStatus(t, authRequest(h, nil, "POST", path, map[string]string{}, nil), 401)
	}
	assertStatus(t, authRequest(h, nil, "POST", "/api/auth/login", map[string]string{"email": "a@example.test", "password": password}, map[string]string{"Origin": "https://attacker.example"}), 403)
	assertStatus(t, authRequest(h, nil, "POST", "/api/auth/login", map[string]string{"email": "a@example.test", "password": password}, map[string]string{"Origin": "", "Referer": ""}), 403)
	a := signIn(t, h, "Alice+canvas@company.test", password)
	b := signIn(t, h, "bob@different-domain.example", password)
	again := signIn(t, h, " alice+canvas@COMPANY.test ", password)
	if a.session.User.ID != again.session.User.ID {
		t.Fatal("existing user duplicated")
	}
	assertStatus(t, authRequest(h, nil, "POST", "/api/auth/login", map[string]string{"email": "alice+canvas@company.test", "password": "incorrect password"}, nil), 401)
	assertStatus(t, authRequest(h, a, "GET", "/api/auth/me", nil, nil), 200)
	var rawHash string
	var verified bool
	if err := db.Pool.QueryRow(ctx, `SELECT password_hash,email_verified_at IS NOT NULL FROM users WHERE id=$1`, a.session.User.ID).Scan(&rawHash, &verified); err != nil || verified || !identity.PasswordMatches(password, rawHash) {
		t.Fatal("password or verification state")
	}
	var storedToken string
	if err := db.Pool.QueryRow(ctx, `SELECT token_hash FROM user_sessions WHERE token_hash=$1`, identity.TokenHash(a.cookie.Value)).Scan(&storedToken); err != nil || storedToken == a.cookie.Value {
		t.Fatal("raw token stored")
	}
	create := map[string]string{"id": assetCanvas, "title": "Alice private"}
	assertStatus(t, authRequest(h, a, "POST", "/api/canvases", create, map[string]string{"X-CSRF-Token": ""}), 403)
	assertStatus(t, authRequest(h, a, "POST", "/api/canvases", create, map[string]string{"Origin": "https://evil.test"}), 403)
	assertStatus(t, authRequest(h, a, "POST", "/api/canvases", create, map[string]string{"X-Session-User": b.session.User.ID}), 409)
	assertStatus(t, authRequest(h, a, "POST", "/api/canvases", create, nil), 201)
	assertStatus(t, authRequest(h, b, "POST", "/api/canvases", create, nil), 404)
	assertStatus(t, authRequest(h, b, "GET", "/api/canvases/"+assetCanvas, nil, nil), 404)
	list := authRequest(h, b, "GET", "/api/canvases", nil, nil)
	assertStatus(t, list, 200)
	if strings.Contains(list.Body.String(), assetCanvas) {
		t.Fatal("cross-user list leak")
	}
	node := persistence.Node{ID: assetNode, Position: persistence.Position{X: 1, Y: 2}, Data: persistence.NodeData{Kind: "image", Name: "private", Origin: "upload"}}
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{node}, Viewport: persistence.Viewport{Zoom: 1}}
	save := map[string]any{"version": 0, "snapshot": snapshot}
	assertStatus(t, authRequest(h, b, "PUT", "/api/canvases/"+assetCanvas, save, nil), 404)
	assertStatus(t, authRequest(h, a, "PUT", "/api/canvases/"+assetCanvas, save, nil), 200)
	assertStatus(t, authRequest(h, b, "POST", "/api/assets?canvasId="+assetCanvas+"&nodeId="+assetNode, nil, map[string]string{"Content-Type": "application/octet-stream"}), 404)
	if objects.uploaded != 0 {
		t.Fatal("unauthorized upload reached OSS")
	}
	asset := storage.Asset{Key: "private-test-key", Scope: storage.Scope{CanvasID: assetCanvas, NodeID: assetNode}}
	if err := db.RecordAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, b, "GET", "/api/assets/content?key=private-test-key", nil, map[string]string{"Range": "bytes=0-3"}), 404)
	media := authRequest(h, a, "GET", "/api/assets/content?key=private-test-key", nil, map[string]string{"Range": "bytes=0-3"})
	assertStatus(t, media, 206)
	if media.Header().Get("Cache-Control") != "private, no-store" || media.Body.String() != "test" {
		t.Fatal("private media caching/range")
	}
	node.Data.Origin = "generated"
	snapshot.Nodes[0] = node
	assertStatus(t, authRequest(h, a, "PUT", "/api/canvases/"+assetCanvas, map[string]any{"version": 1, "snapshot": snapshot}, nil), 200)
	if err := db.GrantCredits(ctx, a.session.User.ID, 20, "auth-ownership-test-grant"); err != nil {
		t.Fatal(err)
	}
	imageQuote, err := db.QuoteCredits(ctx, a.session.User.ID, "image", persistence.Input{CanvasID: assetCanvas, NodeID: assetNode, Prompt: "private prompt", Model: ark.DefaultImageModel})
	if err != nil {
		t.Fatal(err)
	}
	taskID := identity.ID()
	input := map[string]any{"taskId": taskID, "canvasId": assetCanvas, "nodeId": assetNode, "prompt": "private prompt", "model": ark.DefaultImageModel, "acceptedPoints": imageQuote.Points, "priceVersion": imageQuote.PriceVersion}
	assertStatus(t, authRequest(h, b, "POST", "/api/images/generations", input, nil), 404)
	videoInput := map[string]any{"taskId": identity.ID(), "canvasId": assetCanvas, "nodeId": assetNode, "prompt": "private video", "model": ark.VideoModel, "references": []map[string]string{{"nodeId": identity.ID(), "assetKey": "private-test-key", "kind": "image"}}}
	// Use the current supported video model; ownership must fail before storage/provider work.
	assertStatus(t, authRequest(h, b, "POST", "/api/videos/generations", videoInput, nil), 404)
	assertStatus(t, authRequest(h, a, "POST", "/api/images/generations", input, nil), 202)
	assertStatus(t, authRequest(h, b, "POST", "/api/images/generations", input, nil), 404)
	for _, path := range []string{"/api/tasks/" + taskID, "/api/canvases/" + assetCanvas + "/tasks", "/api/canvases/" + assetCanvas + "/tasks?latest=true"} {
		assertStatus(t, authRequest(h, b, "GET", path, nil, nil), 404)
	}
	assertStatus(t, authRequest(h, b, "POST", "/api/tasks/"+taskID+"/storage-retries", map[string]string{}, nil), 404)
	// A signed-in attacker reusing a victim's task ID from their OWN canvas is denied.
	otherID := identity.ID()
	assertStatus(t, authRequest(h, b, "POST", "/api/canvases", map[string]string{"id": otherID}, nil), 201)
	input["canvasId"] = otherID
	assertStatus(t, authRequest(h, b, "POST", "/api/images/generations", input, nil), 404)
	if calls.Load() != 0 {
		t.Fatal("auth tests called a provider")
	}
	// Polling must not extend idle time. A valid mutation extends it, bounded by absolute expiry.
	if _, err := db.Pool.Exec(ctx, `UPDATE user_sessions SET idle_expires_at=now()+interval '1 hour' WHERE token_hash=$1`, identity.TokenHash(a.cookie.Value)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, a, "GET", "/api/auth/me", nil, nil), 200)
	var idle time.Time
	if err := db.Pool.QueryRow(ctx, `SELECT idle_expires_at FROM user_sessions WHERE token_hash=$1`, identity.TokenHash(a.cookie.Value)).Scan(&idle); err != nil || idle.After(time.Now().Add(2*time.Hour)) {
		t.Fatal("read silently renewed idle expiry")
	}
	assertStatus(t, authRequest(h, a, "POST", "/api/canvases", map[string]string{"id": identity.ID()}, nil), 201)
	if err := db.Pool.QueryRow(ctx, `SELECT idle_expires_at FROM user_sessions WHERE token_hash=$1`, identity.TokenHash(a.cookie.Value)).Scan(&idle); err != nil || idle.Before(time.Now().Add(23*time.Hour)) {
		t.Fatal("interaction did not extend idle expiry")
	}
	assertStatus(t, authRequest(h, a, "POST", "/api/auth/logout", nil, nil), 204)
	assertStatus(t, authRequest(h, a, "GET", "/api/auth/me", nil, nil), 401)
	assertStatus(t, authRequest(h, again, "GET", "/api/auth/me", nil, nil), 200)
	if _, err := db.Pool.Exec(ctx, `UPDATE user_sessions SET absolute_expires_at=now()-interval '1 second' WHERE token_hash=$1`, identity.TokenHash(again.cookie.Value)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, again, "GET", "/api/auth/me", nil, nil), 401)
	if _, err := db.Pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, b.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, b, "GET", "/api/auth/me", nil, nil), 401)
	assertStatus(t, authRequest(h, nil, "POST", "/api/auth/login", map[string]string{"email": b.session.User.Email, "password": password}, nil), 401)
}

func TestConcurrentRegistrationNeverReplacesPassword(t *testing.T) {
	db := authDB(t)
	ctx := context.Background()
	email := "race@example.test"
	passwords := []string{"first secret", "second secret"}
	var wg sync.WaitGroup
	results := make([]error, 2)
	sessions := make([]persistence.Session, 2)
	for i := range passwords {
		wg.Add(1)
		go func(i int) { defer wg.Done(); sessions[i], results[i] = db.LoginOrCreate(ctx, email, passwords[i]) }(i)
	}
	wg.Wait()
	winner := -1
	for i, e := range results {
		if e == nil {
			if winner != -1 {
				t.Fatal("different passwords both registered")
			}
			winner = i
		}
	}
	if winner == -1 {
		t.Fatal("no successful registration")
	}
	var n int
	if db.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email=$1`, email).Scan(&n) != nil || n != 1 {
		t.Fatal("duplicate users")
	}
	if _, err := db.LoginOrCreate(ctx, email, passwords[winner]); err != nil {
		t.Fatal("winner password changed")
	}
	if _, err := db.LoginOrCreate(ctx, email, passwords[1-winner]); err != persistence.ErrCredentials {
		t.Fatal("loser password overwrote account")
	}
	if sessions[winner].User.ID == "" {
		t.Fatal("missing user")
	}
}

func TestAuthMissingDatabaseAndLimiter(t *testing.T) {
	h := NewRouter(nil, nil)
	assertStatus(t, authRequest(h, nil, "GET", "/health", nil, nil), 200)
	assertStatus(t, authRequest(h, nil, "GET", "/api/auth/me", nil, nil), 503)
	assertStatus(t, authRequest(h, nil, "POST", "/api/images/generations", nil, nil), 503)
	l := &authLimiter{entries: map[string]attemptBucket{}}
	for range 12 {
		if !l.allow("email", 12) {
			t.Fatal("limit early")
		}
	}
	if l.allow("email", 12) {
		t.Fatal("limit not enforced")
	}
	l.entries["email"] = attemptBucket{Count: 12, Until: time.Now().Add(-time.Second)}
	if !l.allow("email", 12) {
		t.Fatal("limit did not expire")
	}
	cfg := AuthConfig{Secure: true}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	cfg.cookie(c, "test", time.Now().Add(time.Hour))
	cookie := w.Result().Cookies()[0]
	if cookie.Name != "__Host-frame_session" || !cookie.Secure || cookie.Domain != "" {
		t.Fatal("HTTPS cookie policy")
	}
}
