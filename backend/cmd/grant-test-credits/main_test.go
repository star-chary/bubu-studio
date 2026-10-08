package main

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func grantToolDB(t *testing.T) *persistence.Store {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("requires isolated PostgreSQL TEST_DATABASE_URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/frame_space_test" {
		t.Fatal("grant tool tests require local 127.0.0.1/frame_space_test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("test_credit_grant_tool_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := persistence.Open(ctx, u.String())
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); admin.Close() })
	return db
}

func TestGrantToolRejectsSignupCreditForPreviewAndApply(t *testing.T) {
	db := grantToolDB(t)
	ctx := context.Background()
	session, err := db.LoginOrCreate(ctx, "signup-grant-tool@example.test", "signup password")
	if err != nil {
		t.Fatal(err)
	}
	for _, apply := range []bool{false, true} {
		var output bytes.Buffer
		err := grantOne(ctx, db, apply, &output)
		if err == nil || !strings.Contains(err.Error(), "已获得注册赠送") || output.Len() != 0 {
			t.Fatalf("apply=%v: expected explicit refusal without output, got %v, %q", apply, err, output.String())
		}
	}
	account, err := db.Credits(ctx, session.User.ID)
	if err != nil || account != (persistence.CreditAccount{Available: persistence.SignupTestCredits}) {
		t.Fatalf("signup account changed: %+v, %v", account, err)
	}
	var grants int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_ledger WHERE user_id=$1`, session.User.ID).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("unexpected signup grant count: %d, %v", grants, err)
	}
}

func TestGrantToolKeepsLegacyKeyIdempotent(t *testing.T) {
	db := grantToolDB(t)
	ctx := context.Background()
	userID := identity.ID()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'unused')`, userID, "legacy-grant-tool@example.test"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := grantOne(ctx, db, false, &output); err != nil || !strings.Contains(output.String(), "--apply 将幂等发放 200") {
		t.Fatalf("legacy preview: %v, %q", err, output.String())
	}
	for range 2 {
		output.Reset()
		if err := grantOne(ctx, db, true, &output); err != nil || !strings.Contains(output.String(), "可用=200") {
			t.Fatalf("legacy apply: %v, %q", err, output.String())
		}
	}
	var grants int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_ledger WHERE user_id=$1 AND idempotency_key=$2`, userID, "initial-test-credits-20261006:"+userID).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("legacy idempotency key: count=%d, %v", grants, err)
	}
}
