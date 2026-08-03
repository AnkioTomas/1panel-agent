package main

import (
	"fmt"
	"os"

	"1panel-agent/internal/agent"
	"1panel-agent/internal/config"
	"1panel-agent/internal/master"
)

// Set by release builds: -ldflags "-X main.version=v1.2.3"
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "master":
		if len(os.Args) >= 3 && os.Args[2] == "set" {
			if err := runMasterSet(os.Args[3:]); err != nil {
				fatal(err)
			}
			return
		}
		if err := runMaster(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "agent":
		if err := runAgent(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runMaster(args []string) error {
	opts := master.Options{
		Takeover: true,
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen":
			i++
			if i >= len(args) {
				return fmt.Errorf("--listen needs a value")
			}
			opts.Listen = args[i]
		case "--token":
			i++
			if i >= len(args) {
				return fmt.Errorf("--token needs a value")
			}
			opts.Token = args[i]
		case "--host":
			i++
			if i >= len(args) {
				return fmt.Errorf("--host needs a value")
			}
			opts.PublicHost = args[i]
		case "--panel-user":
			i++
			if i >= len(args) {
				return fmt.Errorf("--panel-user needs a value")
			}
			opts.PanelUser = args[i]
		case "--panel-pass":
			i++
			if i >= len(args) {
				return fmt.Errorf("--panel-pass needs a value")
			}
			opts.PanelPass = args[i]
		case "--entrance":
			i++
			if i >= len(args) {
				return fmt.Errorf("--entrance needs a value")
			}
			opts.Entrance = args[i]
		case "--no-takeover":
			opts.Takeover = false
		case "--upstream":
			i++
			if i >= len(args) {
				return fmt.Errorf("--upstream needs a value")
			}
			opts.LocalPanel = args[i]
		default:
			return fmt.Errorf("unknown master flag: %s", args[i])
		}
	}

	srv, err := master.New(opts)
	if err != nil {
		return err
	}
	return srv.Run()
}

func runMasterSet(args []string) error {
	st, err := config.LoadMasterOrEmpty()
	if err != nil {
		return err
	}
	changed := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--host":
			i++
			if i >= len(args) {
				return fmt.Errorf("--host needs a value")
			}
			st.PublicHost = args[i]
			changed = true
		case "--panel-user":
			i++
			if i >= len(args) {
				return fmt.Errorf("--panel-user needs a value")
			}
			st.PanelUser = args[i]
			changed = true
		case "--panel-pass":
			i++
			if i >= len(args) {
				return fmt.Errorf("--panel-pass needs a value")
			}
			st.PanelPassword = args[i]
			changed = true
		case "--token":
			i++
			if i >= len(args) {
				return fmt.Errorf("--token needs a value")
			}
			st.Token = args[i]
			changed = true
		case "--entrance":
			i++
			if i >= len(args) {
				return fmt.Errorf("--entrance needs a value")
			}
			st.Entrance = args[i]
			changed = true
		default:
			return fmt.Errorf("unknown master set flag: %s", args[i])
		}
	}
	if !changed {
		return fmt.Errorf("usage: 1pm master set [--host IP] [--panel-pass P] [--panel-user U] [--token T] [--entrance E]")
	}
	if err := config.SaveMaster(st); err != nil {
		return err
	}
	path, _ := config.MasterPath()
	fmt.Printf("master config saved: %s\n", path)
	return nil
}

func runAgent(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("agent needs subcommand: register|set|run")
	}
	switch args[0] {
	case "register":
		if len(args) != 2 {
			return fmt.Errorf("usage: agent register host:port/token")
		}
		return agent.RegisterAndRun(args[1])
	case "set":
		return runAgentSet(args[1:])
	case "run":
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w (run agent register first)", err)
		}
		agent.AutofillPanel(cfg)
		return agent.Run(cfg)
	default:
		return fmt.Errorf("unknown agent subcommand: %s", args[0])
	}
}

func runAgentSet(args []string) error {
	var panelURL, panelKey string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--panel-url":
			i++
			if i >= len(args) {
				return fmt.Errorf("--panel-url needs a value")
			}
			panelURL = args[i]
		case "--panel-key":
			i++
			if i >= len(args) {
				return fmt.Errorf("--panel-key needs a value")
			}
			panelKey = args[i]
		default:
			return fmt.Errorf("unknown set flag: %s", args[i])
		}
	}
	if panelURL == "" && panelKey == "" {
		return fmt.Errorf("usage: agent set --panel-url URL [--panel-key KEY]")
	}
	if err := agent.SetPanel(panelURL, panelKey); err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Printf("config saved: %s\n", path)
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `1pm %s — 1Panel multi-node tunnel (Master + Agent)

Usage:
  1pm master                                          # systemd: ExecStart=/usr/local/bin/1pm master
  curl -fsSL "http://<master>:<port>/agent.sh?token=<TOKEN>" | sudo bash
  1pm version

Master UI: http://<master>:<panel-port>/__mp/
  Auth = your 1Panel browser session (no password stored by 1pm).
`, version)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
