package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"1panel-agent/internal/agent"
	"1panel-agent/internal/buildinfo"
	"1panel-agent/internal/config"
	"1panel-agent/internal/master"
	"1panel-agent/internal/role"

	"golang.org/x/term"
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
	case "uninstall":
		if err := runUninstall(); err != nil {
			fatal(err)
		}
	case "version", "-v", "--version":
		fmt.Println(buildinfo.Version)
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

// runAgent 处理 agent 子命令（install/run/setpwd）。
func runAgent(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("agent needs subcommand: install|run|setpwd")
	}
	switch args[0] {
	case "install":
		return runAgentInstall(args[1:])
	case "run":
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w (run agent install first)", err)
		}
		if err := agent.AutofillPanel(cfg); err != nil {
			return err
		}
		return agent.Run(cfg)
	case "setpwd":
		return runAgentSetpwd(args[1:])
	default:
		return fmt.Errorf("unknown agent subcommand: %s", args[0])
	}
}

// runUninstall 按本机已安装角色自动卸载（master / agent / 两者）。
func runUninstall() error {
	hasMaster := role.MasterPresent()
	hasAgent := role.AgentPresent()
	if !hasMaster && !hasAgent {
		return fmt.Errorf("未检测到 1pm master 或 agent，无需卸载")
	}
	if hasAgent {
		log.Println("uninstall: detected agent")
		if err := agent.Clean(); err != nil {
			return err
		}
	}
	if hasMaster {
		log.Println("uninstall: detected master")
		if err := master.Clean(); err != nil {
			return err
		}
	}
	removeSelfBin()
	switch {
	case hasMaster && hasAgent:
		fmt.Println("1pm master + agent uninstalled.")
	case hasMaster:
		fmt.Println("1pm master uninstalled.")
	default:
		fmt.Println("1pm agent uninstalled.")
	}
	return nil
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

// runAgentInstall 安装时写入 Master/Token（不启动长连接）。
func runAgentInstall(args []string) error {
	var positional []string
	opts := agent.InstallOpts{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			i++
			if i >= len(args) {
				return fmt.Errorf("--name needs a value")
			}
			opts.Name = args[i]
		case "--group":
			i++
			if i >= len(args) {
				return fmt.Errorf("--group needs a value")
			}
			opts.Group = args[i]
		case "--master-tls":
			opts.MasterTLS = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown install flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if opts.Name == "" {
		opts.Name = os.Getenv("NODE_NAME")
	}
	if opts.Group == "" {
		opts.Group = os.Getenv("NODE_GROUP")
	}
	if !opts.MasterTLS && os.Getenv("MASTER_TLS") == "1" {
		opts.MasterTLS = true
	}
	switch len(positional) {
	case 2:
		return agent.Install(positional[0], positional[1], opts)
	default:
		return fmt.Errorf("usage: agent install <host:port> <token> [--name NAME] [--group GROUP]")
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
	fmt.Printf("password verified & saved (encrypted) to %s\n", path)
	return nil
}

// usage 打印命令行帮助信息。
func usage() {
	fmt.Fprintf(os.Stderr, `1pm %s — 1Panel multi-node tunnel (Master + Agent)

Usage:
  1pm master                          start master
  1pm uninstall                       auto-detect and uninstall master and/or agent

  1pm agent install <host:port> <token> [--name NAME] [--group GROUP]
                                      write config at install time (panel URL/user auto-detected)
  1pm agent run                       start agent with saved config
  1pm agent setpwd [--password PASS]  set 1Panel password (encrypted at rest)

  1pm version

Master UI: http://<master>:<panel-port>/__mp/
  Agent download / WS auth = HMAC timestamp+sign (not raw token query).
  One host cannot run master and agent at the same time.
`, buildinfo.Version)
}

// fatal 打印错误信息并退出程序。
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
