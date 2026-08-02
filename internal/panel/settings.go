package panel

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

const DefaultCoreDB = "/opt/1panel/db/core.db"

type Settings struct {
	ServerPort        int
	SecurityEntrance  string
	UserName          string
	PasswordPublicKey string
	SystemVersion     string
}

func ReadSettings(dbPath string) (*Settings, error) {
	if dbPath == "" {
		dbPath = DefaultCoreDB
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT key, value FROM settings WHERE key IN (
		'ServerPort','SecurityEntrance','UserName','PASSWORD_PUBLIC_KEY','SystemVersion')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	port, _ := strconv.Atoi(m["ServerPort"])
	if port == 0 {
		return nil, fmt.Errorf("ServerPort missing in %s", dbPath)
	}
	return &Settings{
		ServerPort:        port,
		SecurityEntrance:  m["SecurityEntrance"],
		UserName:          m["UserName"],
		PasswordPublicKey: m["PASSWORD_PUBLIC_KEY"],
		SystemVersion:     m["SystemVersion"],
	}, nil
}

var versionLineRe = regexp.MustCompile(`版本:\s*(v?[0-9][^\s]*)`)

// ReadSystemVersion returns local 1Panel version from core.db, or 1pctl fallback.
func ReadSystemVersion(dbPath string) string {
	if dbPath == "" {
		dbPath = DefaultCoreDB
	}
	if st, err := ReadSettings(dbPath); err == nil && strings.TrimSpace(st.SystemVersion) != "" {
		return strings.TrimSpace(st.SystemVersion)
	}
	out, err := exec.Command("1pctl", "version").CombinedOutput()
	if err != nil {
		return ""
	}
	if m := versionLineRe.FindSubmatch(out); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

func UpdateServerPort(dbPath string, port int) error {
	if dbPath == "" {
		dbPath = DefaultCoreDB
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`UPDATE settings SET value = ? WHERE key = 'ServerPort'`, strconv.Itoa(port))
	return err
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

func WaitListen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	_ = ln.Close()
	return nil
}
