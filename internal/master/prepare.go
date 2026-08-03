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

// EnsureTakeover makes local 1Panel listen on an internal port and returns public listen port.
func EnsureTakeover(state *config.Master) (publicPort, internalPort int, entrance string, err error) {
	st, err := panel.ReadSettings()
	if err != nil {
		return 0, 0, "", fmt.Errorf("read 1panel settings: %w", err)
	}
	entrance = st.SecurityEntrance

	if state.OriginalPort > 0 && state.InternalPort > 0 {
		publicPort = state.OriginalPort
		internalPort = state.InternalPort
		if st.ServerPort != internalPort {
			log.Printf("takeover: restore internal port %d (panel has %d)", internalPort, st.ServerPort)
			if err := panel.UpdateServerPort(internalPort); err != nil {
				return 0, 0, "", err
			}
			_ = restartPanel()
		}
	} else {
		publicPort = st.ServerPort
		internalPort = panel.InternalPort(publicPort)
		if internalPort < 1 || internalPort > 65535 {
			return 0, 0, "", fmt.Errorf("cannot derive a valid internal port from public port %d (got %d)", publicPort, internalPort)
		}
		if st.ServerPort != internalPort {
			log.Printf("takeover: move 1Panel %d -> 127.0.0.1:%d", publicPort, internalPort)
			if err := panel.UpdateServerPort(internalPort); err != nil {
				return 0, 0, "", err
			}
			if err := restartPanel(); err != nil {
				return 0, 0, "", err
			}
		}
		state.OriginalPort = publicPort
		state.InternalPort = internalPort
		if err := config.SaveMaster(state); err != nil {
			log.Printf("warn: save master state: %v", err)
		}
	}

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", internalPort), 45*time.Second); err != nil {
		return 0, 0, "", err
	}
	return publicPort, internalPort, entrance, nil
}

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
