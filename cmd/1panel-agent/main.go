package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"1panel-agent/internal/agent"
	"1panel-agent/internal/config"
	"1panel-agent/internal/master"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "master":
		if err := runMaster(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "agent":
		if err := runAgent(os.Args[2:]); err != nil {
			fatal(err)
		}
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
		DBPath:   "",
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

	if opts.Token == "" {
		if st, err := config.LoadMaster(); err == nil && st.Token != "" {
			opts.Token = st.Token
		} else {
			tok, err := randomToken()
			if err != nil {
				return err
			}
			opts.Token = tok
			fmt.Fprintf(os.Stderr, "generated token: %s\n", opts.Token)
		}
	}
	if opts.PanelUser == "" {
		if st, err := config.LoadMaster(); err == nil {
			opts.PanelUser = st.PanelUser
			if opts.PanelPass == "" {
				opts.PanelPass = st.PanelPassword
			}
		}
	}

	srv, err := master.New(opts)
	if err != nil {
		return err
	}
	// persist credentials/token
	st, _ := config.LoadMasterOrEmpty()
	st.Token = srv.Token
	st.PanelUser = srv.PanelUser
	st.PanelPassword = srv.PanelPass
	st.Entrance = srv.Entrance
	st.PublicHost = srv.PublicHost
	_ = config.SaveMaster(st)

	return srv.Run()
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
	var panelURL, panelKey, panelUser, panelPass, entrance string
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
		case "--panel-user":
			i++
			if i >= len(args) {
				return fmt.Errorf("--panel-user needs a value")
			}
			panelUser = args[i]
		case "--panel-pass":
			i++
			if i >= len(args) {
				return fmt.Errorf("--panel-pass needs a value")
			}
			panelPass = args[i]
		case "--entrance":
			i++
			if i >= len(args) {
				return fmt.Errorf("--entrance needs a value")
			}
			entrance = args[i]
		default:
			return fmt.Errorf("unknown set flag: %s", args[i])
		}
	}
	if panelURL == "" && panelKey == "" && panelUser == "" && panelPass == "" && entrance == "" {
		return fmt.Errorf("usage: agent set --panel-url URL [--panel-user U --panel-pass P --entrance E]")
	}
	if err := agent.SetPanel(panelURL, panelKey, panelUser, panelPass, entrance); err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Printf("config saved: %s\n", path)
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `1pm — 1Panel multi-node tunnel (Master + Agent)

Usage:
  1pm master --host 10.211.55.14 --token SECRET --panel-user USER --panel-pass PASS
      # default: takeover local 1Panel port, reverse-proxy local panel, UI at /__mp/

  1pm agent register host:port/token
  1pm agent set --panel-url http://127.0.0.1:52045 --panel-user U --panel-pass P
  1pm agent run

Master UI: http://<master>:<panel-port>/__mp/
`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
