package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
)

func adminSignIn(t *testing.T, h http.Handler, db *persistence.Store) *authClient {
	t.Helper()
	ordinary := signIn(t, h, "operator@example.test", "admin password")
	if err := db.PromoteAdmin(context.Background(), ordinary.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, ordinary, "GET", "/api/auth/me", nil, nil), 401)
	w := authRequest(h, nil, "POST", "/api/admin/auth/login", map[string]string{"email": "operator@example.test", "password": "admin password"}, nil)
	assertStatus(t, w, 200)
	var session persistence.Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil || session.User.Role != "admin" {
		t.Fatalf("admin login: %s", w.Body)
	}
	if !w.Result().Cookies()[0].HttpOnly {
		t.Fatal("admin session is not HttpOnly")
	}
	return &authClient{w.Result().Cookies()[0], session}
}

func TestAdminLoginCannotRegisterOrElevate(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &stubAssets{}, db)
	ordinary := signIn(t, h, "ordinary@example.test", "regular password")
	for _, email := range []string{"unknown@example.test", "ordinary@example.test"} {
		w := authRequest(h, nil, "POST", "/api/admin/auth/login", map[string]string{"email": email, "password": "regular password"}, nil)
		assertStatus(t, w, 401)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("unauthorized admin login issued cookie")
		}
	}
	var users, grants int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&users); err != nil || users != 1 {
		t.Fatalf("admin login created a user: %d %v", users, err)
	}
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM credit_ledger`).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("admin login granted credits: %d %v", grants, err)
	}
	assertStatus(t, authRequest(h, ordinary, "GET", "/api/admin/users", nil, nil), 403)
	admin := adminSignIn(t, h, db)
	assertStatus(t, authRequest(h, nil, "POST", "/api/admin/auth/login", map[string]string{"email": "operator@example.test", "password": "wrong password"}, nil), 401)
	if err := db.PromoteAdmin(context.Background(), admin.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, admin, "GET", "/api/admin/me", nil, nil), 200)
}

func TestAdminRoutesEnforceRoleCSRFAndFreshPermissions(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &stubAssets{}, db)
	ctx := context.Background()
	admin := adminSignIn(t, h, db)
	user := signIn(t, h, "creator@example.test", "regular password")
	path := "/api/admin/users/" + user.session.User.ID + "/credits"
	body := map[string]any{"points": 50, "reason": "测试补充", "requestId": identity.ID()}
	for _, path := range []string{"/api/admin/me", "/api/admin/users", "/api/admin/users/" + user.session.User.ID + "/ledger"} {
		assertStatus(t, authRequest(h, nil, "GET", path, nil, nil), 401)
		assertStatus(t, authRequest(h, user, "GET", path, nil, nil), 403)
	}
	assertStatus(t, authRequest(h, user, "POST", path, body, nil), 403)
	assertStatus(t, authRequest(h, admin, "POST", path, body, map[string]string{"X-CSRF-Token": ""}), 403)
	assertStatus(t, authRequest(h, admin, "POST", path, body, map[string]string{"Origin": "https://attacker.example"}), 403)
	assertStatus(t, authRequest(h, admin, "POST", path, body, map[string]string{"X-Session-User": user.session.User.ID}), 409)
	canvas := identity.ID()
	if err := db.CreateCanvas(ctx, canvas, "private", user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, admin, "GET", "/api/canvases/"+canvas, nil, nil), 404)
	if _, err := db.Pool.Exec(ctx, `UPDATE users SET role='user' WHERE id=$1`, admin.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, admin, "GET", "/api/admin/users", nil, nil), 403)
	assertStatus(t, authRequest(h, admin, "POST", path, body, nil), 403)
	if _, err := db.Pool.Exec(ctx, `UPDATE users SET role='admin',status='disabled' WHERE id=$1`, admin.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, admin, "GET", "/api/admin/users", nil, nil), 401)
	account, err := db.Credits(ctx, user.session.User.ID)
	if err != nil || account.Available != persistence.SignupTestCredits {
		t.Fatalf("unauthorized grant changed account: %+v %v", account, err)
	}
}

func TestAdminUsersSearchPaginationAndGrantAudit(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &stubAssets{}, db)
	ctx := context.Background()
	admin := adminSignIn(t, h, db)
	user := signIn(t, h, "creator@example.test", "regular password")
	legacyID := identity.ID()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'legacy@example.test','not-a-public-field')`, legacyID); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		path         string
		count, total int
	}{
		{"/api/admin/users", 3, 3}, {"/api/admin/users?limit=1&offset=1", 1, 3}, {"/api/admin/users?q=CREATOR", 1, 1}, {"/api/admin/users?q=" + user.session.User.ID, 1, 1}, {"/api/admin/users?q=%25", 0, 0}, {"/api/admin/users?offset=50", 0, 3},
	} {
		w := authRequest(h, admin, "GET", target.path, nil, nil)
		assertStatus(t, w, 200)
		var value struct {
			Users []persistence.AdminUser `json:"users"`
			Total int                     `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil || len(value.Users) != target.count || value.Total != target.total {
			t.Fatalf("list: %s %v", w.Body, err)
		}
		if strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "token") || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private response contract")
		}
	}
	for _, q := range []string{"?limit=0", "?limit=101", "?offset=-1", "?offset=abc", "?q=" + strings.Repeat("a", 255)} {
		assertStatus(t, authRequest(h, admin, "GET", "/api/admin/users"+q, nil, nil), 400)
	}
	path := "/api/admin/users/" + user.session.User.ID + "/credits"
	body := map[string]any{"points": 50, "reason": "  体验测试补充  ", "requestId": identity.ID()}
	var firstID int64
	for i := range 2 {
		w := authRequest(h, admin, "POST", path, body, nil)
		assertStatus(t, w, 200)
		var result persistence.AdminGrantResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Account.Available != 250 || result.Account.Reserved != 0 || result.Replayed != (i == 1) || result.Entry.ActorID == nil || *result.Entry.ActorID != admin.session.User.ID || result.Entry.Reason != "体验测试补充" || result.Entry.Source != "manual" {
			t.Fatalf("grant result: %+v", result)
		}
		if i == 0 {
			firstID = result.Entry.ID
		} else if result.Entry.ID != firstID {
			t.Fatal("retry created new entry")
		}
	}
	body["points"] = 51
	assertStatus(t, authRequest(h, admin, "POST", path, body, nil), 409)
	ledger := authRequest(h, admin, "GET", "/api/admin/users/"+user.session.User.ID+"/ledger?limit=1", nil, nil)
	assertStatus(t, ledger, 200)
	var records struct {
		Entries []persistence.AdminCreditEntry `json:"entries"`
		HasMore bool                           `json:"hasMore"`
	}
	if err := json.Unmarshal(ledger.Body.Bytes(), &records); err != nil || len(records.Entries) != 1 || !records.HasMore || records.Entries[0].ID != firstID {
		t.Fatalf("ledger pagination: %s %v", ledger.Body, err)
	}
	publicLedger := authRequest(h, user, "GET", "/api/credits/ledger", nil, nil)
	assertStatus(t, publicLedger, 200)
	if strings.Contains(publicLedger.Body.String(), "operator@example.test") || strings.Contains(publicLedger.Body.String(), "体验测试补充") {
		t.Fatal("internal audit leaked to ordinary user")
	}
	w := authRequest(h, user, "GET", "/api/credits", nil, nil)
	assertStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"available":250`) {
		t.Fatalf("user cannot see grant: %s", w.Body)
	}
	// A legacy account without a credit_accounts row can also receive a grant.
	body["requestId"] = identity.ID()
	assertStatus(t, authRequest(h, admin, "POST", "/api/admin/users/"+legacyID+"/credits", body, nil), 200)
}

func TestAdminGrantRejectsInvalidAndDisabledTargets(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &stubAssets{}, db)
	admin := adminSignIn(t, h, db)
	user := signIn(t, h, "creator@example.test", "regular password")
	ctx := context.Background()
	path := "/api/admin/users/" + user.session.User.ID + "/credits"
	for _, points := range []any{0, -1, 10001, 1.5, "20"} {
		assertStatus(t, authRequest(h, admin, "POST", path, map[string]any{"points": points, "reason": "test", "requestId": identity.ID()}, nil), 400)
	}
	for _, reason := range []string{"", "   ", strings.Repeat("测", 201)} {
		assertStatus(t, authRequest(h, admin, "POST", path, map[string]any{"points": 20, "reason": reason, "requestId": identity.ID()}, nil), 400)
	}
	body := map[string]any{"points": 20, "reason": "test", "requestId": ""}
	assertStatus(t, authRequest(h, admin, "POST", path, body, nil), 400)
	body["requestId"] = identity.ID()
	assertStatus(t, authRequest(h, admin, "POST", "/api/admin/users/"+identity.ID()+"/credits", body, nil), 404)
	if _, err := db.Pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, authRequest(h, admin, "POST", path, body, nil), 409)
	account, err := db.Credits(ctx, user.session.User.ID)
	if err != nil || account.Available != persistence.SignupTestCredits {
		t.Fatalf("invalid grant changed balance: %+v %v", account, err)
	}
}
