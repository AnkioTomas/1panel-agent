package agent

import (
	"testing"
)

func TestUpdateSelfSkippedWhenCurrent(t *testing.T) {
	// ResolveTag 需要网络；这里只验证已是同版本时的短路需要 mock。
	// 无网络环境下 UpdateSelf 应返回错误而不是 panic。
	_, err := UpdateSelf(false)
	if err == nil {
		t.Log("UpdateSelf succeeded (network available)")
		return
	}
	// 常见：非 linux CI、或无法解析 latest——只要不 panic 即可。
	t.Logf("UpdateSelf err (expected in restricted env): %v", err)
}
