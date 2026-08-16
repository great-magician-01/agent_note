package main

import (
	"log"
	"os"

	"github.com/great-magician-01/agent_note/internal/config"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/router"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"github.com/great-magician-01/agent_note/internal/worker"
)

func main() {
	config.Load()

	// 确保运行目录存在
	for _, dir := range []string{config.C.LogDir, config.C.UploadDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("[main] mkdir %s failed: %v", dir, err)
		}
	}

	snowflake.Init(config.C.SnowflakeNode)
	database.Init()
	worker.Start()

	r := router.Setup()
	addr := ":" + config.C.ServerPort
	log.Printf("[main] server listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("[main] server failed: %v", err)
	}
}
