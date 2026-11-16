package desktop

import (
	"encoding/json"
	"os"
	"strings"
)

// App is one entry of the image's apps.json.
type App struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	NameZH   string   `json:"nameZh,omitempty"`
	Exec     []string `json:"exec"`
	CLI      string   `json:"cli,omitempty"`
	Category string   `json:"category"`
	Icon     string   `json:"icon,omitempty"`
}

// LoadApps reads the catalog. A missing or broken file is an empty catalog.
func LoadApps(path string) []App {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw []App
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	out := make([]App, 0, len(raw))
	seen := map[string]bool{}
	for _, a := range raw {
		a.ID = strings.TrimSpace(a.ID)
		if a.ID == "" || seen[a.ID] || len(a.Exec) == 0 || strings.TrimSpace(a.Exec[0]) == "" {
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	return out
}

// InstalledApps keeps the apps whose executable resolves.
func InstalledApps(apps []App, lookPath func(string) (string, error)) []App {
	out := make([]App, 0, len(apps))
	for _, a := range apps {
		if _, err := lookPath(a.Exec[0]); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// FindApp returns the app with this id.
func FindApp(apps []App, id string) (App, bool) {
	id = strings.TrimSpace(id)
	for _, a := range apps {
		if a.ID == id {
			return a, true
		}
	}
	return App{}, false
}
