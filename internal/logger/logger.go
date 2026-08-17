// Package logger 按天切分的文件日志：标准 log 包同时输出到 stdout 与 logs/log_yyyyMMdd.log。
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// dailyWriter 实现 io.Writer，按日期切分日志文件，跨天自动关闭旧文件并打开新文件
type dailyWriter struct {
	dir     string
	mu      sync.Mutex
	curDate string
	file    *os.File
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format("20060102")
	if w.file == nil || w.curDate != today {
		if w.file != nil {
			_ = w.file.Close()
			w.file = nil
		}
		f, err := os.OpenFile(
			filepath.Join(w.dir, "log_"+today+".log"),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644,
		)
		if err != nil {
			return 0, fmt.Errorf("打开日志文件失败: %w", err)
		}
		w.file = f
		w.curDate = today
	}
	return w.file.Write(p)
}

// Init 初始化全局日志：创建日志目录，并将标准 log 输出同时写到 stdout 与按天切分的日志文件
func Init(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dw := &dailyWriter{dir: dir}
	// 预热打开当天文件，尽早暴露权限等问题
	if _, err := dw.Write(nil); err != nil {
		return err
	}
	log.SetOutput(io.MultiWriter(os.Stdout, dw))
	return nil
}
