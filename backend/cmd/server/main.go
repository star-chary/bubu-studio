package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/server"
	"frame-space/backend/internal/storage"
	"github.com/joho/godotenv"
)

func main() {
	// 环境变量优先；本地密钥文件位于 Git 忽略范围，不参与前端构建。
	if err := godotenv.Load(".env.local"); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal("无法读取后端 .env.local，请检查文件格式。")
	}
	if _, err := server.AuthConfigFromEnv(); err != nil {
		log.Fatal(err)
	}
	if _, err := server.TrustedProxiesFromEnv(); err != nil {
		log.Fatal(err)
	}
	workerConfig, err := persistence.WorkerConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// 仅监听本机，由同机 Nginx 提供公网入口；端口被占用时不自动切换。
	address := net.JoinHostPort("127.0.0.1", port)
	configuredStorage, err := storage.NewFromEnv()
	if err != nil {
		log.Fatalf("OSS 配置无效：%s", err.Error())
	}
	var assetStore server.AssetStore
	if configuredStorage != nil {
		assetStore = configuredStorage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var db *persistence.Store
	generator := ark.NewClient(os.Getenv("ARK_API_KEY"))
	workerDone := make(chan struct{})
	if url := os.Getenv("DATABASE_URL"); url != "" {
		connectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		db, err = persistence.Open(connectCtx, url)
		cancel()
		if err != nil {
			log.Fatal("PostgreSQL 连接或迁移失败，请检查 DATABASE_URL 与数据库服务。")
		}
		defer db.Close()
		ready := make(chan error, 1)
		go func() {
			defer close(workerDone)
			if err := db.RunWorker(ctx, generator, assetStore, ready, workerConfig); err != nil && ctx.Err() == nil {
				log.Print("任务执行器停止，请检查数据库连接后重启后端。")
				stop()
			}
		}()
		if err := <-ready; err != nil {
			log.Fatal("任务执行器启动失败，请检查是否已有后端连接同一个数据库。")
		}
		log.Print("PostgreSQL 已连接，画布保存与后台任务已启用。")
		log.Printf("任务并发：视频 %d，图片 %d，素材转存 %d；超出的任务持久化排队。", workerConfig.Videos, workerConfig.Images, workerConfig.Saves)
	} else {
		close(workerDone)
		log.Print("未配置 DATABASE_URL：登录与业务接口不可用，请配置数据库。")
	}
	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.NewRouter(generator, assetStore, db),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       120 * time.Second,
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Print("HTTP 请求未在关闭期限内完成，正在关闭剩余连接。")
			_ = httpServer.Close()
		}
	}()

	log.Printf("帧间后端启动地址：http://%s", address)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("后端启动失败：%v", err)
	}
	stop()
	<-shutdownDone
	<-workerDone
}
