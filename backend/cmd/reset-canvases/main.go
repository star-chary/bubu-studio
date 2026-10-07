// Explicit one-time removal of the legacy, ownerless canvas data. Normal server
// startup never deletes data. OSS keys are retained in a local backup manifest.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "delete legacy records after saving a local backup")
	expected := flag.String("database", "frame_space", "expected database name")
	flag.Parse()
	_ = godotenv.Load(".env.local")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("无法连接项目数据库")
	}
	defer db.Close(ctx)
	tx, err := db.Begin(ctx)
	if err != nil {
		log.Fatal("无法开启事务")
	}
	defer tx.Rollback(context.Background())
	var name string
	if tx.QueryRow(ctx, `SELECT current_database()`).Scan(&name) != nil || name != *expected {
		log.Fatal("数据库名称不匹配，未清理")
	}
	var owned bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='canvases' AND column_name='owner_user_id')`).Scan(&owned) != nil || owned {
		log.Fatal("只允许清理账号功能之前的旧画布；未清理")
	}
	var locked bool
	if tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended(current_schema() || ':frame-space-worker',0))`).Scan(&locked) != nil || !locked {
		log.Fatal("请先停止本项目后端，再清理旧画布")
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE canvases,assets,generation_tasks IN ACCESS EXCLUSIVE MODE`); err != nil {
		log.Fatal("无法锁定旧画布，未清理")
	}
	backup := map[string][]json.RawMessage{}
	for _, table := range []string{"canvases", "assets", "generation_tasks"} {
		rows, e := tx.Query(ctx, `SELECT row_to_json(t) FROM `+pgx.Identifier{table}.Sanitize()+` t`)
		if e != nil {
			log.Fatal("无法读取旧数据")
		}
		backup[table] = []json.RawMessage{}
		for rows.Next() {
			var raw []byte
			if rows.Scan(&raw) != nil {
				log.Fatal("无法读取旧数据")
			}
			backup[table] = append(backup[table], json.RawMessage(raw))
		}
		if rows.Err() != nil {
			log.Fatal("无法读取旧数据")
		}
		rows.Close()
		fmt.Printf("%s: %d records\n", table, len(backup[table]))
	}
	if !*apply {
		fmt.Println("只读检查完成；未删除数据库或 OSS 文件。")
		return
	}
	dir, err := filepath.Abs("../.data/auth-reset")
	if err != nil {
		log.Fatal("无法解析备份路径")
	}
	if os.MkdirAll(dir, 0700) != nil {
		log.Fatal("无法创建备份目录")
	}
	data, err := json.Marshal(backup)
	if err != nil {
		log.Fatal("无法序列化备份")
	}
	path := filepath.Join(dir, "legacy-canvases-"+time.Now().UTC().Format("20060102T150405Z")+".json")
	if os.WriteFile(path, data, 0600) != nil {
		log.Fatal("无法写入备份；未删除")
	}
	for _, table := range []string{"generation_tasks", "assets", "canvases"} {
		if _, err = tx.Exec(ctx, `DELETE FROM `+pgx.Identifier{table}.Sanitize()); err != nil {
			log.Fatal("清理失败，事务回滚")
		}
	}
	if tx.Commit(ctx) != nil {
		log.Fatal("清理事务提交失败")
	}
	fmt.Println("旧画布及关联记录已清理；本地备份已保存。私有 OSS 原文件保留，不再由应用提供访问。")
}
