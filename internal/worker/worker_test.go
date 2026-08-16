package worker

import (
	"strings"
	"sync"
	"testing"
)

// resetState 每个用例前重置包级状态：重建队列与去重表
func resetState(capacity int) {
	queue = make(chan int64, capacity)
	pending = sync.Map{}
}

// TestTruncateShort 短串（含恰好等于上限）原样返回
func TestTruncateShort(t *testing.T) {
	if got := truncate("你好", 8000); got != "你好" {
		t.Fatalf("truncate(短串) = %q，期望原样返回", got)
	}
	s := strings.Repeat("汉", 10)
	if got := truncate(s, 10); got != s {
		t.Fatal("长度恰好等于上限时不应截断")
	}
}

// TestTruncateLong 超长串截断到指定 rune 数并追加标记；中文按 rune 截断不断码
func TestTruncateLong(t *testing.T) {
	const suffix = "\n…(内容过长已截断)"
	s := strings.Repeat("汉", 100)
	got := truncate(s, 10)
	want := strings.Repeat("汉", 10) + suffix
	if got != want {
		t.Fatalf("truncate 结果 = %q，期望 %q", got, want)
	}
	if !strings.HasPrefix(got, strings.Repeat("汉", 10)) || !strings.HasSuffix(got, suffix) {
		t.Fatalf("截断结果缺少前缀/后缀: %q", got)
	}
}

// TestEnqueueDedup 同一 noteID 重复投递只入队一次；不同 id 都能入队
func TestEnqueueDedup(t *testing.T) {
	resetState(8)

	Enqueue(42)
	Enqueue(42) // 去重，不重复入队
	if len(queue) != 1 {
		t.Fatalf("同一 id 投两次后 len(queue) = %d，期望 1", len(queue))
	}
	if _, ok := pending.Load(int64(42)); !ok {
		t.Fatal("入队后 pending 应有 42 的标记")
	}

	Enqueue(43)
	if len(queue) != 2 {
		t.Fatalf("不同 id 入队后 len(queue) = %d，期望 2", len(queue))
	}
	if _, ok := pending.Load(int64(43)); !ok {
		t.Fatal("入队后 pending 应有 43 的标记")
	}
}

// TestEnqueueQueueFull 队列满时丢弃该 id 并清除 pending 标记，腾位置后可再次入队
func TestEnqueueQueueFull(t *testing.T) {
	resetState(1)

	Enqueue(9) // 占满唯一位置
	if len(queue) != 1 {
		t.Fatalf("len(queue) = %d，期望 1", len(queue))
	}

	// 队列满：丢弃并清除标记（会打 [worker] queue full 日志，属正常）
	Enqueue(10)
	if len(queue) != 1 {
		t.Fatalf("队列满时 len(queue) = %d，期望仍为 1", len(queue))
	}
	if _, ok := pending.Load(int64(10)); ok {
		t.Fatal("丢弃后 pending 不应残留 10 的标记")
	}

	// 再次投递同一 id：因标记已清除会重新尝试入队，队列仍满再次被丢弃，pending 无残留
	Enqueue(10)
	if _, ok := pending.Load(int64(10)); ok {
		t.Fatal("再次丢弃后 pending 不应残留 10 的标记")
	}

	// 消费一个腾出位置后可成功入队
	if id := <-queue; id != 9 {
		t.Fatalf("出队 id = %d，期望 9", id)
	}
	Enqueue(10)
	if len(queue) != 1 {
		t.Fatalf("腾位置后 len(queue) = %d，期望 1", len(queue))
	}
	if _, ok := pending.Load(int64(10)); !ok {
		t.Fatal("成功入队后 pending 应有 10 的标记")
	}
}
