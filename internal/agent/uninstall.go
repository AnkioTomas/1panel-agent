package agent

import (
	"log"
	"os"
	"os/exec"

	"1panel-agent/internal/config"
)

// agentServiceFile 是 Agent 的 systemd unit 路径。
const agentServiceFile = "/etc/systemd/system/1pm-agent.service"

// Clean 停止 agent 服务并清除配置/unit，保留二进制。
// 由全局 `1pm uninstall` 调用；二进制删除在 CLI 统一处理。
func Clean() error {
	quietExec("systemctl", "stop", "1pm-agent.service")
	quietExec("systemctl", "disable", "1pm-agent.service")
	log.Println("uninstall: stopped 1pm-agent.service")

	_ = os.Remove(agentServiceFile)
	quietExec("systemctl", "daemon-reload")
	log.Printf("uninstall: removed %s", agentServiceFile)

	if dir, err := config.Dir(); err == nil {
		if err := os.RemoveAll(dir); err == nil {
			log.Printf("uninstall: removed config dir %s", dir)
		}
	}
	return nil
}

// quietExec 静默执行 Shell 命令输出。
func quietExec(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
