package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"
)

// collectHostStats 采集本机 CPU/内存与版本信息。
func collectHostStats() protocol.HostStats {
	st := protocol.HostStats{
		AgentVersion: buildinfo.Version,
		PanelVersion: panel.ReadSystemVersion(),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
	}
	st.MemTotal, st.MemUsed = readMem()
	st.CPUPercent = sampleCPUPercent(120 * time.Millisecond)
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
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		v *= 1024 // kB -> bytes
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
		if i == 4 { // idle
			idle = n
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
	busy := totalDelta - idleDelta
	if busy < 0 {
		busy = 0
	}
	return busy * 100 / totalDelta
}
