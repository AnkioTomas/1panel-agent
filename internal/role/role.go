// Package role 检测本机 1pm 角色（master / agent），禁止同机双角色。
package role

import (
	"fmt"
	"os"
)

// 与安装脚本一致的生产路径（仅这些路径参与互斥判断）。
const (
	MasterUnit  = "/etc/systemd/system/1pm-master.service"
	AgentUnit   = "/etc/systemd/system/1pm-agent.service"
	MasterState = "/var/lib/1pm/master.json"
	AgentState  = "/root/.1panel-agent/agent.json"
)

// MasterPresent 判断本机是否已安装 Master。
func MasterPresent() bool {
	return fileExists(MasterUnit) || fileExists(MasterState)
}

// AgentPresent 判断本机是否已安装 Agent。
func AgentPresent() bool {
	return fileExists(AgentUnit) || fileExists(AgentState)
}

// RefuseMasterIfAgent 若已装 Agent 则拒绝安装/启动 Master。
func RefuseMasterIfAgent() error {
	if AgentPresent() {
		return fmt.Errorf("本机已安装 1pm agent，不能同时作为 master；先执行: 1pm uninstall")
	}
	return nil
}

// RefuseAgentIfMaster 若已装 Master 则拒绝安装/启动 Agent。
func RefuseAgentIfMaster() error {
	if MasterPresent() {
		return fmt.Errorf("本机已安装 1pm master，不能同时作为 agent；先执行: 1pm uninstall")
	}
	return nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
