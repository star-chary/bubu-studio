// Grant the initial test allowance only when the configured database contains
// exactly one user. Re-running --apply is idempotent for that same user.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
	var count int
	var userID string
	if err = db.Pool.QueryRow(ctx, `SELECT count(*)::int FROM users`).Scan(&count); err != nil || count != 1 {
		log.Fatalf("仅允许当前数据库恰好有一位用户时发放；当前用户数=%d", count)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT id::text FROM users WHERE status='active'`).Scan(&userID); err != nil {
		log.Fatal("唯一用户未处于可用状态")
	}
	account, err := db.Credits(ctx, userID)
	if err != nil {
		log.Fatal("无法读取积分账户")
	}
	if !*apply {
		fmt.Printf("预览：用户数=1，当前可用=%d，冻结=%d；--apply 将幂等发放 200 测试积分。\n", account.Available, account.Reserved)
		return
	}
	if err = db.GrantCredits(ctx, userID, 200, "initial-test-credits-20261006:"+userID); err != nil {
		log.Fatal("发放失败：", err)
	}
	account, err = db.Credits(ctx, userID)
	if err != nil {
		log.Fatal("发放后读取积分失败")
	}
	fmt.Printf("发放完成或已存在：用户数=1，可用=%d，冻结=%d。\n", account.Available, account.Reserved)
}
