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

// InjectSidebarMenu adds "多机节点" into local 1Panel HideMenu (master or agent).
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
	for _, m := range menus {
		if m.ID == multiPanelMenuID || m.Path == "/__mp/" || m.Label == "MultiPanel-Menu" {
			log.Printf("sidebar menu already present")
			return nil
		}
	}
	item := showMenu{
		ID:       multiPanelMenuID,
		Label:    "MultiPanel-Menu",
		Disabled: false,
		IsShow:   true,
		Title:    "多机节点",
		Path:     "/__mp/",
		Sort:     1050,
	}
	out := make([]showMenu, 0, len(menus)+1)
	inserted := false
	for _, m := range menus {
		if !inserted && m.ID == "13" {
			out = append(out, item)
			inserted = true
		}
		out = append(out, m)
	}
	if !inserted {
		out = append(out, item)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE settings SET value = ? WHERE key = 'HideMenu'`, string(data))
	if err != nil {
		return err
	}
	log.Printf("injected sidebar menu: 多机节点 -> /__mp/")
	return nil
}
