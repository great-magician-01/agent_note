package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/great-magician-01/agent_note/internal/config"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/logger"
	"github.com/great-magician-01/agent_note/internal/router"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"github.com/great-magician-01/agent_note/internal/worker"
)

func main() {
	config.Load()

	// 初始化按天切分的文件日志（失败仅警告，不影响启动）
	if err := logger.Init(config.C.LogDir); err != nil {
		log.Printf("[main] 日志文件初始化失败，仅输出到 stdout: %v", err)
	}

	// 确保上传目录存在（日志目录已由 logger.Init 创建）
	if err := os.MkdirAll(config.C.UploadDir, 0o755); err != nil {
		log.Fatalf("[main] mkdir %s failed: %v", config.C.UploadDir, err)
	}

	snowflake.Init(config.C.SnowflakeNode)
	database.Init()
	worker.Start()

	r := router.Setup()
	addr := ":" + config.C.ServerPort
	srv := &http.Server{Addr: addr, Handler: r}

	// 优雅停机：收到 SIGINT/SIGTERM 后停止接收新请求，等待进行中请求结束（最多 5s）
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("[main] server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[main] server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("[main] 收到停机信号，正在优雅停机…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[main] 优雅停机失败: %v", err)
	}
	log.Println("[main] 已停机")
}
