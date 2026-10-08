package persistence

import (
	"context"
	"errors"
	"sync"
	"testing"

	"frame-space/backend/internal/identity"
)

func adminFixture(t *testing.T, db *Store) string {
	t.Helper()
	id := identity.ID()
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO users(id,email,password_hash,role) VALUES($1,'admin@example.test','unused','admin')`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAdminGrantConcurrentRetriesAndGeneration(t *testing.T) {
	db := testDB(t)
	actor := adminFixture(t, db)
	ctx := context.Background()
	if err := db.GrantCredits(ctx, testUserID, 100, "seed"); err != nil {
		t.Fatal(err)
	}
	quote, err := db.QuoteCredits(ctx, testUserID, "image", input())
	if err != nil {
		t.Fatal(err)
	}
	key := identity.ID()
	errs := make(chan error, 13)
	results := make(chan AdminGrantResult, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			result, err := db.AdminGrantCredits(ctx, actor, testUserID, 25, "并发体验补充", key)
			errs <- err
			results <- result
		})
	}
	wg.Go(func() {
		_, err := db.CreateBilledTask(ctx, testUserID, identity.ID(), "image", input(), quote.Points, quote.PriceVersion)
		errs <- err
	})
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	created := 0
	var entryID int64
	for result := range results {
		if !result.Replayed {
			created++
		}
		if entryID == 0 {
			entryID = result.Entry.ID
		}
		if result.Entry.ID != entryID {
			t.Fatal("retry returned different ledger")
		}
	}
	if created != 1 {
		t.Fatalf("actual grants: %d", created)
	}
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{Available: 119, Reserved: 6}) {
		t.Fatalf("concurrent balance: %+v %v", account, err)
	}
	for _, change := range []struct {
		points int64
		reason string
	}{{26, "并发体验补充"}, {25, "different"}} {
		if _, err := db.AdminGrantCredits(ctx, actor, testUserID, change.points, change.reason, key); !errors.Is(err, ErrConflict) {
			t.Fatalf("different payload reused key: %v", err)
		}
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_ledger WHERE source='manual'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("ledger count: %d %v", count, err)
	}
}

func TestAdminGrantRollsBackBalanceWhenAuditFails(t *testing.T) {
	db := testDB(t)
	actor := adminFixture(t, db)
	ctx := context.Background()
	if err := db.GrantCredits(ctx, testUserID, 80, "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `ALTER TABLE credit_ledger ADD CONSTRAINT reject_test_manual CHECK (source <> 'manual')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdminGrantCredits(ctx, actor, testUserID, 30, "should rollback", identity.ID()); err == nil {
		t.Fatal("expected audit failure")
	}
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{Available: 80}) {
		t.Fatalf("partial grant: %+v %v", account, err)
	}
}

func TestAdminGrantRechecksActorAndSupportsDistinctConcurrentGrants(t *testing.T) {
	db := testDB(t)
	actor := adminFixture(t, db)
	ctx := context.Background()
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := db.AdminGrantCredits(ctx, actor, testUserID, 10, "separate grant", identity.ID())
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account.Available != 80 {
		t.Fatalf("lost grant: %+v %v", account, err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE users SET role='user' WHERE id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdminGrantCredits(ctx, actor, testUserID, 10, "unauthorized", identity.ID()); !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("demoted actor could grant: %v", err)
	}
}
