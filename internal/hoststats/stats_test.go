package hoststats

import (
	"runtime"
	"testing"
	"time"
)

func TestCollectCPUMemLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	st := Collect()
	if st.MemTotal == 0 {
		t.Fatal("mem total 0")
	}
	if st.CPUPercent < 0 || st.CPUPercent > 100 {
		t.Fatalf("cpu out of range: %v", st.CPUPercent)
	}
	// Under load should often be >0; idle may be 0 — just ensure sampling finishes.
	time.Sleep(10 * time.Millisecond)
}
