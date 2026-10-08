// Initialize an administrator by an explicit existing user ID. No public route
// can promote accounts. Preview does not migrate or mutate the database.
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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	id := flag.String("user-id", "", "existing user UUID to authorize")
	apply := flag.Bool("apply", false, "authorize the account and revoke its old sessions")
	flag.Parse()
	if !persistence.ValidID(*id) {
		log.Fatal("必须用 --user-id 指定已核对的用户 UUID")
	}
	if err := godotenv.Load(".env.local"); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal("无法读取本地配置")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("未配置 DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatal("数据库配置无效")
	}
	defer pool.Close()
	db := &persistence.Store{Pool: pool}
	user, err := db.AdminUser(ctx, *id)
	if err != nil {
		log.Fatal("用户不存在或数据库尚未应用 007_admin.sql，请先检查迁移和用户 ID")
	}
	fmt.Printf("用户：%s；ID：%s；角色：%s；状态：%s\n", user.Email, user.ID, user.Role, user.Status)
	if !*apply {
		fmt.Println("预览完成。添加 --apply 后授权管理员，并撤销该账号原有会话；重复授权不重复撤销。")
		return
	}
	if err = db.PromoteAdmin(ctx, user.ID); err != nil {
		log.Fatal("授权失败，请检查账号是否处于 active 状态")
	}
	fmt.Println("管理员已就绪，请在 BuBu-后台管理重新登录。")
}
