package main

import (
	"fmt"
	"os"

	"1panel-agent/internal/agent"
	"1panel-agent/internal/config"
	"1panel-agent/internal/master"

	"golang.org/x/term"
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
		if len(os.Args) >= 3 {
			switch os.Args[2] {
			case "uninstall":
				if err := master.Uninstall(); err != nil {
					fatal(err)
				}
				return
			}
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

// runMaster 启动 Master 服务。
func runMaster(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("master does not take arguments")
	}
	srv, err := master.New()
	if err != nil {
		return err
	}
	return srv.Run()
}

// runAgent 处理 agent 子命令（install/run/setpwd/uninstall）。
func runAgent(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("agent needs subcommand: install|run|setpwd|uninstall")
	}
	switch args[0] {
	case "install":
		return runAgentInstall(args[1:])
	case "run":
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w (run agent install first)", err)
		}
		agent.AutofillPanel(cfg)
		return agent.Run(cfg)
	case "setpwd":
		return runAgentSetpwd(args[1:])
	case "uninstall":
		return agent.Uninstall()
	default:
		return fmt.Errorf("unknown agent subcommand: %s", args[0])
	}
}

// runAgentInstall 安装时写入 Master/Token（不启动长连接）。
func runAgentInstall(args []string) error {
	switch len(args) {
	case 1:
		return agent.InstallFromTarget(args[0])
	case 2:
		return agent.Install(args[0], args[1])
	default:
		return fmt.Errorf("usage: agent install <host:port> <token> | agent install host:port/token")
	}
}

// runAgentSetpwd 交互或参数设置加密保存的面板密码。
func runAgentSetpwd(args []string) error {
	var pass string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--password":
			i++
			if i >= len(args) {
				return fmt.Errorf("--password needs a value")
			}
			pass = args[i]
		default:
			return fmt.Errorf("unknown setpwd flag: %s", args[i])
		}
	}
	if pass == "" {
		if v := os.Getenv("PANEL_PASS"); v != "" {
			pass = v
		}
	}
	if pass == "" {
		fmt.Fprint(os.Stderr, "1Panel password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		pass = string(b)
	}
	if err := agent.SetPassword(pass); err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Printf("password saved (encrypted) to %s\n", path)
	return nil
}

// usage 打印命令行帮助信息。
func usage() {
	fmt.Fprintf(os.Stderr, `1pm %s — 1Panel multi-node tunnel (Master + Agent)

Usage:
  1pm master                          start master
  1pm master uninstall                stop service, restore 1Panel port, remove state & binary

  1pm agent install <host:port> <token>
                                      write config at install time (panel URL/user auto-detected)
  1pm agent run                       start agent with saved config
  1pm agent setpwd [--password PASS]  set 1Panel password (encrypted at rest)
  1pm agent uninstall                 stop service, remove config & binary

  1pm version

Master UI: http://<master>:<panel-port>/__mp/
  Agent download / WS auth = HMAC timestamp+sign (not raw token query).
`, version)
}

// fatal 打印错误信息并退出程序。
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
