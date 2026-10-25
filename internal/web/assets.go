package web

import (
	"encoding/json"
	"io/fs"
)

// assetManifest is the subset of Vite's manifest we need. Styles stay at a
// stable path so they can be cached by the service worker; scripts are hashed.
type assetManifest struct {
	File string `json:"file"`
}

type assets struct {
	Script string
}

// loadAssets reads the Vite manifest embedded with the binary. If the frontend
// has not been built yet (for example `go run` during development), we fall
// back to the legacy unhashed bundle so the app still works.
func loadAssets() assets {
	out := assets{Script: "/static/app.js"}
	raw, err := fs.ReadFile(staticFS, "static/.vite/manifest.json")
	if err != nil {
		return out
	}
	var manifest map[string]assetManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return out
	}
	if entry, ok := manifest["src/main.ts"]; ok && entry.File != "" {
		out.Script = "/static/" + entry.File
	}
	return out
}

var uiAssets = loadAssets()
