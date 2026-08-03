// Package hoststats 采集本机 CPU / 内存，供 Master 与 Agent 共用。
package hoststats

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Stats 是一次主机资源快照。
type Stats struct {
	CPUPercent float64
	MemTotal   uint64
	MemUsed    uint64
	GOOS       string
	GOARCH     string
}

// Collect 采集本机 CPU/内存。CPU 用两次 /proc/stat 差值估算。
func Collect() Stats {
	st := Stats{
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
	}
	st.MemTotal, st.MemUsed = readMem()
	st.CPUPercent = sampleCPUPercent(300 * time.Millisecond)
	return st
}

func readMem() (total, used uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		v *= 1024
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total >= avail {
		used = total - avail
	}
	return total, used
}

// readCPUTimes 返回 idle（含 iowait）与 total jiffies。
func readCPUTimes() (idle, total uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(sc.Text())
	// cpu user nice system idle iowait irq softirq steal guest guest_nice
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var sum uint64
	for i := 1; i < len(fields); i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		sum += n
	}
	idleOnly, _ := strconv.ParseUint(fields[4], 10, 64)
	idle = idleOnly
	if len(fields) > 5 {
		iowait, err := strconv.ParseUint(fields[5], 10, 64)
		if err == nil {
			idle += iowait
		}
	}
	return idle, sum, true
}

func sampleCPUPercent(d time.Duration) float64 {
	idle1, total1, ok := readCPUTimes()
	if !ok {
		return 0
	}
	time.Sleep(d)
	idle2, total2, ok := readCPUTimes()
	if !ok || total2 <= total1 {
		return 0
	}
	idleDelta := float64(idle2 - idle1)
	totalDelta := float64(total2 - total1)
	if totalDelta <= 0 {
		return 0
	}
	busy := totalDelta - idleDelta
	if busy < 0 {
		busy = 0
	}
	pct := busy * 100 / totalDelta
	// 保留一位小数，避免前端看到一长串
	return float64(int(pct*10+0.5)) / 10
}
