package master

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"1panel-agent/internal/config"
	"1panel-agent/internal/panel"
)

const masterServiceFile = "/etc/systemd/system/1pm-master.service"

// Uninstall stops the master service, restores the original 1Panel port,
// removes state files, and removes the binary itself.
func Uninstall() error {
	quietExec("systemctl", "stop", "1pm-master.service")
	quietExec("systemctl", "disable", "1pm-master.service")
	log.Println("uninstall: stopped 1pm-master.service")

	// Restore original 1Panel port if we know what it was.
	if state, err := config.LoadMaster(); err == nil && state.OriginalPort > 0 {
		st, err := panel.ReadSettings()
		if err == nil && st.ServerPort != state.OriginalPort {
			log.Printf("uninstall: restoring 1Panel port %d → %d", st.ServerPort, state.OriginalPort)
			if err := panel.UpdateServerPort(state.OriginalPort); err != nil {
				log.Printf("warn: restore panel port: %v", err)
			} else {
				quietExec("systemctl", "restart", "1panel-core")
				log.Printf("uninstall: 1panel-core restarted on port %d", state.OriginalPort)
			}
		}
	}

	_ = os.Remove(masterServiceFile)
	quietExec("systemctl", "daemon-reload")
	log.Printf("uninstall: removed %s", masterServiceFile)

	if p, err := config.MasterPath(); err == nil {
		dir := filepath.Dir(p)
		if err := os.RemoveAll(dir); err == nil {
			log.Printf("uninstall: removed state dir %s", dir)
		}
	}

	removeSelfBin()

	fmt.Println("1pm master uninstalled.")
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
