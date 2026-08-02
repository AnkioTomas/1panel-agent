package panel

import (
	"database/sql"
	"encoding/json"
	"log"

	_ "modernc.org/sqlite"
)

const multiPanelMenuID = "91"

type showMenu struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Disabled bool       `json:"disabled"`
	IsShow   bool       `json:"isShow"`
	Title    string     `json:"title"`
	Path     string     `json:"path"`
	Sort     int        `json:"sort,omitempty"`
	Children []showMenu `json:"children,omitempty"`
}

// InjectSidebarMenu cleans stale HideMenu entries for「多机节点」.
// Real sidebar entry is HTML/JS inject (Vue router cannot host /__mp/).
func InjectSidebarMenu(dbPath string) error {
	if dbPath == "" {
		dbPath = DefaultCoreDB
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	var raw string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'HideMenu'`).Scan(&raw); err != nil {
		return err
	}
	var menus []showMenu
	if err := json.Unmarshal([]byte(raw), &menus); err != nil {
		return err
	}
	out := make([]showMenu, 0, len(menus))
	removed := 0
	for _, m := range menus {
		if m.ID == multiPanelMenuID || m.Path == "/__mp/" || m.Label == "MultiPanel-Menu" {
			removed++
			continue
		}
		out = append(out, m)
	}
	if removed == 0 {
		return nil
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if _, err = db.Exec(`UPDATE settings SET value = ? WHERE key = 'HideMenu'`, string(data)); err != nil {
		return err
	}
	log.Printf("removed stale HideMenu entry 多机节点 (x%d); sidebar uses HTML inject only", removed)
	return nil
}
