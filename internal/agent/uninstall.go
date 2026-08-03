package agent

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"1panel-agent/internal/config"
)

const agentServiceFile = "/etc/systemd/system/1pm-agent.service"

// Uninstall stops the agent service, removes config, and optionally removes
// the binary itself.
func Uninstall(removeBin bool) error {
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

	if removeBin {
		removeSelfBin()
	}

	fmt.Println("1pm agent uninstalled.")
	return nil
}

func quietExec(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func removeSelfBin() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("warn: locate binary: %v", err)
		return
	}
	if err := os.Remove(exe); err != nil {
		log.Printf("warn: remove binary %s: %v", exe, err)
		return
	}
	log.Printf("uninstall: removed binary %s", exe)
}
