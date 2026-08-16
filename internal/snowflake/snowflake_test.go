package snowflake

import (
	"sync"
	"testing"
)

// TestNextPositiveAndUnique 验证 Next 返回正数，且连续两次生成的 ID 不同
func TestNextPositiveAndUnique(t *testing.T) {
	first := Next()
	if first <= 0 {
		t.Fatalf("Next() = %d，期望正数", first)
	}
	second := Next()
	if second <= 0 {
		t.Fatalf("Next() = %d，期望正数", second)
	}
	if first == second {
		t.Fatalf("连续两次 Next() 结果相同: %d", first)
	}
}

// TestNextConcurrentUnique 8 个 goroutine 各生成 1000 个 ID，channel 汇总后用 map 判重，不允许重复
func TestNextConcurrentUnique(t *testing.T) {
	const goroutines = 8
	const perGoroutine = 1000

	ch := make(chan int64, goroutines*perGoroutine)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				ch <- Next()
			}
		}()
	}
	wg.Wait()
	close(ch)

	seen := make(map[int64]struct{}, goroutines*perGoroutine)
	count := 0
	for id := range ch {
		if _, dup := seen[id]; dup {
			t.Fatalf("发现重复 ID: %d", id)
		}
		seen[id] = struct{}{}
		count++
	}
	if count != goroutines*perGoroutine {
		t.Fatalf("共收集 %d 个 ID，期望 %d", count, goroutines*perGoroutine)
	}
}
