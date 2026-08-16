package snowflake

import (
	"log"
	"sync"

	"github.com/bwmarrin/snowflake"
)

var (
	node *snowflake.Node
	once sync.Once
)

// Init 初始化雪花节点（单机部署 nodeID 默认 1）
func Init(nodeID int64) {
	once.Do(func() {
		n, err := snowflake.NewNode(nodeID)
		if err != nil {
			log.Fatalf("[snowflake] init failed: %v", err)
		}
		node = n
	})
}

// Next 生成下一个雪花 ID（int64）
func Next() int64 {
	if node == nil {
		Init(1)
	}
	return node.Generate().Int64()
}
