package agent

import (
	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/hoststats"
	"1panel-agent/internal/panel"
	"1panel-agent/internal/protocol"
)

// collectHostStats 采集本机 CPU/内存与版本信息。
func collectHostStats() protocol.HostStats {
	hs := hoststats.Collect()
	return protocol.HostStats{
		CPUPercent:   hs.CPUPercent,
		MemTotal:     hs.MemTotal,
		MemUsed:      hs.MemUsed,
		AgentVersion: buildinfo.Version,
		PanelVersion: panel.ReadSystemVersion(),
		GOOS:         hs.GOOS,
		GOARCH:       hs.GOARCH,
	}
}
