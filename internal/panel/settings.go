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

type Settings struct {
	ServerPort       int
	SecurityEntrance string
	UserName         string
	SystemVersion    string
}

var (
	versionLineRe = regexp.MustCompile(`(?m)(?:版本|Version)\s*:\s*(v?[0-9][^\s]*)`)
	userLineRe    = regexp.MustCompile(`(?m)(?:用户|User)\s*:\s*(\S+)`)
	// 1panel user-info: "Panel address: http://$LOCAL_IP:62045/tomas"
	panelURLRe = regexp.MustCompile(`https?://[^\s]+`)
)

// ReadSettings reads port/entrance/user via 1panel CLI. Never opens core.db.
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

// ReadSystemVersion returns local 1Panel version from 1pctl version.
func ReadSystemVersion() string {
	out, err := exec.Command("1pctl", "version").CombinedOutput()
	if err != nil {
		out, err = runPanel("version")
		if err != nil {
			return ""
		}
	}
	if m := versionLineRe.FindSubmatch(stripANSIBytes(out)); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// UpdateServerPort changes panel listen port via official CLI (stdin to update port).
// 1panel always exits 0; success/failure is in the text. Same-port "occupied" is OK.
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

func runPanel(args ...string) ([]byte, error) {
	cmd := exec.Command("1panel", append([]string{"-l", "en"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, nil
	}
	// 1pctl for environments where 1panel binary name differs
	out2, err2 := exec.Command("1pctl", args...).CombinedOutput()
	if err2 != nil {
		return out, fmt.Errorf("%v (%s); 1pctl: %v (%s)", err, bytes.TrimSpace(out), err2, bytes.TrimSpace(out2))
	}
	return out2, nil
}

func LocalPanelURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func InternalPort(publicPort int) int {
	// Keep within uint16 and avoid collision with common services.
	p := publicPort + 100000
	if p > 65535 {
		p = publicPort + 10000
	}
	if p > 65535 {
		p = 152045
	}
	return p
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func stripANSIBytes(b []byte) []byte {
	return []byte(stripANSI(string(b)))
}
