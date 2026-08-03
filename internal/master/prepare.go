package master

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
)

// EnsureTakeover 确保本地 1Panel 运行在内部避让端口，并返回对外公网端口及面板基本信息。
func EnsureTakeover(state *config.Master) (publicPort, internalPort int, entrance, panelUser string, err error) {
	st, err := panel.ReadSettings()
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("read 1panel settings: %w", err)
	}
	entrance = st.SecurityEntrance
	panelUser = st.UserName

	if state.OriginalPort == 0 || state.InternalPort == 0 {
		state.OriginalPort = st.ServerPort
		state.InternalPort = panel.InternalPort(st.ServerPort)
		if err := config.SaveMaster(state); err != nil {
			log.Printf("warn: save master state: %v", err)
		}
	}

	publicPort = state.OriginalPort
	internalPort = state.InternalPort

	if st.ServerPort != internalPort {
		log.Printf("takeover: move 1Panel %d -> 127.0.0.1:%d", st.ServerPort, internalPort)
		if err := panel.UpdateServerPort(internalPort); err != nil {
			return 0, 0, "", "", err
		}
		if err := restartPanel(); err != nil {
			return 0, 0, "", "", err
		}
	}

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", internalPort), 45*time.Second); err != nil {
		return 0, 0, "", "", err
	}
	return publicPort, internalPort, entrance, panelUser, nil
}

// restartPanel 重启 1Panel 相关 systemd 单元（失败只打日志）。
func restartPanel() error {
	for _, unit := range []string{"1panel-core", "1panel-agent"} {
		cmd := exec.Command("systemctl", "restart", unit)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("warn: systemctl restart %s: %v", unit, err)
		}
	}
	return nil
}

// waitPort 在 timeout 内轮询 TCP 端口就绪。
func waitPort(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		time.Sleep(400 * time.Millisecond)
	}
	return fmt.Errorf("wait %s: %v", addr, last)
}
