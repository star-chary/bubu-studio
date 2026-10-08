// Grant the initial test allowance only when the configured database contains
// exactly one legacy user. Newly registered users receive it at signup.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"frame-space/backend/internal/persistence"
	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "grant the one existing user 200 test credits")
	flag.Parse()
	if err := godotenv.Load(".env.local"); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal("无法读取本地数据库配置")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("未配置 DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := persistence.Open(ctx, url)
	if err != nil {
		log.Fatal("数据库连接或迁移失败")
	}
	defer db.Close()
	if err := grantOne(ctx, db, *apply, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func grantOne(ctx context.Context, db *persistence.Store, apply bool, output io.Writer) error {
	var count int
	var userID string
	if err := db.Pool.QueryRow(ctx, `SELECT count(*)::int FROM users`).Scan(&count); err != nil || count != 1 {
		return fmt.Errorf("仅允许当前数据库恰好有一位用户时发放；当前用户数=%d", count)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE status='active'`).Scan(&userID); err != nil {
		return errors.New("唯一用户未处于可用状态")
	}
	account, err := db.Credits(ctx, userID)
	if err != nil {
		return errors.New("无法读取积分账户")
	}
	grantedAtSignup, err := hasSignupWelcomeGrant(ctx, db, userID)
	if err != nil {
		return errors.New("无法核对注册赠分流水")
	}
	if grantedAtSignup {
		return errors.New("该账号已获得注册赠送的 200 测试积分，旧补发工具拒绝再次发放")
	}
	if !apply {
		fmt.Fprintf(output, "预览：用户数=1，当前可用=%d，冻结=%d；--apply 将幂等发放 200 测试积分。\n", account.Available, account.Reserved)
		return nil
	}
	if err = db.GrantCredits(ctx, userID, 200, "initial-test-credits-20261006:"+userID); err != nil {
		return fmt.Errorf("发放失败：%w", err)
	}
	account, err = db.Credits(ctx, userID)
	if err != nil {
		return errors.New("发放后读取积分失败")
	}
	fmt.Fprintf(output, "发放完成或已存在：用户数=1，可用=%d，冻结=%d。\n", account.Available, account.Reserved)
	return nil
}

func hasSignupWelcomeGrant(ctx context.Context, db *persistence.Store, userID string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM credit_ledger WHERE user_id=$1 AND idempotency_key=$2
	)`, userID, "signup-welcome-v1:"+userID).Scan(&exists)
	return exists, err
}
