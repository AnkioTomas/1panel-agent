package main

import (
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
	listen := ":8080"
	token := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen":
			i++
			if i >= len(args) {
				return fmt.Errorf("--listen needs a value")
			}
			listen = args[i]
		case "--token":
			i++
			if i >= len(args) {
				return fmt.Errorf("--token needs a value")
			}
			token = args[i]
		default:
			return fmt.Errorf("unknown master flag: %s", args[i])
		}
	}
	if token == "" {
		return fmt.Errorf("--token is required")
	}
	return master.New(listen, token).Run()
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
		return fmt.Errorf("usage: agent set --panel-url URL --panel-key KEY")
	}
	if err := agent.SetPanel(panelURL, panelKey); err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Printf("config saved: %s\n", path)
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `1panel-agent — multi-node 1Panel tunnel

Usage:
  1panel-agent master --listen :8080 --token SECRET
  1panel-agent agent register host:port/token
  1panel-agent agent set --panel-url http://127.0.0.1:20560 --panel-key KEY
  1panel-agent agent run
`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
