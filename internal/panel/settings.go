// Package panel 封装与本机 1Panel 的 CLI/HTTP 交互（设置、登录、鉴权头、加密）。
package panel

import (
	"bytes"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Settings 封装本地 1Panel 系统的基础配置信息。
type Settings struct {
	ServerPort       int
	SecurityEntrance string
	UserName         string
	SystemVersion    string
}

// CLI 输出解析用正则。
var (
	versionLineRe = regexp.MustCompile(`(?mi)^(?:版本|version)\s*:\s*(v?[0-9][^\s]*)`)
	userLineRe    = regexp.MustCompile(`(?m)(?:用户|user)\s*:\s*(\S+)`)
	// 1panel user-info: "Panel address: http://$LOCAL_IP:62045/tomas"
	panelURLRe = regexp.MustCompile(`https?://[^\s]+`)
)

// ReadSettings 通过 1panel CLI 读取端口、安全入口与用户名等面板配置信息。
func ReadSettings() (*Settings, error) {
	out, err := runPanel("user-info")
	if err != nil {
		return nil, fmt.Errorf("1panel user-info: %w", err)
	}
	st, err := parseUserInfo(out)
	if err != nil {
		return nil, err
	}
	st.SystemVersion = ReadSystemVersion()
	return st, nil
}

// parseUserInfo 解析 1panel user-info 输出内容中的端口、安全入口与用户名。
func parseUserInfo(out []byte) (*Settings, error) {
	text := stripANSI(string(out))
	st := &Settings{}
	if m := userLineRe.FindStringSubmatch(text); len(m) == 2 {
		st.UserName = m[1]
	}
	for _, raw := range panelURLRe.FindAllString(text, -1) {
		u, err := url.Parse(raw)
		if err != nil || u.Port() == "" {
			continue
		}
		port, err := strconv.Atoi(u.Port())
		if err != nil || port <= 0 {
			continue
		}
		st.ServerPort = port
		st.SecurityEntrance = strings.TrimPrefix(u.Path, "/")
		st.SecurityEntrance = strings.TrimSuffix(st.SecurityEntrance, "/")
		break
	}
	if st.ServerPort == 0 {
		return nil, fmt.Errorf("parse user-info: panel port missing\n%s", text)
	}
	return st, nil
}

// ReadSystemVersion 通过 1panel version 读取本地 1Panel 版本号。
func ReadSystemVersion() string {
	out, err := runPanel("version")
	if err != nil {
		return ""
	}
	if m := versionLineRe.FindSubmatch(stripANSIBytes(out)); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// UpdateServerPort 通过 1panel CLI 修改面板监听端口。
func UpdateServerPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}
	if st, err := ReadSettings(); err == nil && st.ServerPort == port {
		return nil
	}
	cmd := exec.Command("1panel", "-l", "en", "update", "port")
	cmd.Stdin = strings.NewReader(strconv.Itoa(port) + "\n")
	out, _ := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(stripANSIBytes(out)))
	if strings.Contains(msg, "Update successful") || strings.Contains(msg, "修改成功") {
		return nil
	}
	// occupied / garbled — accept only if panel already listens on target
	if st, err := ReadSettings(); err == nil && st.ServerPort == port {
		return nil
	}
	if msg == "" {
		msg = "no success message from 1panel update port"
	}
	return fmt.Errorf("update port: %s", msg)
}

// runPanel 统一调用 1panel 命令行工具并返回输出结果。
func runPanel(args ...string) ([]byte, error) {
	cmd := exec.Command("1panel", append([]string{"-l", "en"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("1panel %s: %w (%s)", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

// LocalPanelURL 生成指定端口的本地 1Panel HTTP 地址。
func LocalPanelURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// InternalPort 根据公网端口推算避免冲突的内部端口。
func InternalPort(publicPort int) int {
	// Prefer moving up by 10000; if that overflows, move down by 10000.
	// Either way the result must be a valid port and differ from publicPort.
	p := publicPort + 10000
	if p > 65535 {
		p = publicPort - 10000
	}
	return p
}

// ansiRe 匹配终端 ANSI 颜色转义序列。
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI 过滤字符串中的 ANSI 转义颜色字符。
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// stripANSIBytes 过滤字节切片中的 ANSI 转义颜色字符。
func stripANSIBytes(b []byte) []byte {
	return []byte(stripANSI(string(b)))
}
