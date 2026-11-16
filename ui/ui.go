package ui

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed dist/*
var distFS embed.FS

// GetFS returns the filesystem pointing inside the dist folder.
func GetFS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return distFS
	}
	return sub
}

// ReadFile reads a file from the embedded dist directory.
func ReadFile(path string) ([]byte, error) {
	return distFS.ReadFile(fmt.Sprintf("dist/%s", path))
}

// GetBundledHTML produces a single self-contained HTML document with inlined CSS & JS.
func GetBundledHTML() (string, error) {
	indexHTMLBytes, err := ReadFile("index.html")
	if err != nil {
		return "", fmt.Errorf("index.html okunamadı: %w", err)
	}
	html := string(indexHTMLBytes)

	// Inlined themes.css
	themesCSS, err := ReadFile("css/themes.css")
	if err == nil {
		html = strings.Replace(html, `<link rel="stylesheet" href="css/themes.css">`, `<style>`+string(themesCSS)+`</style>`, 1)
	}

	// Inlined main.css
	mainCSS, err := ReadFile("css/main.css")
	if err == nil {
		html = strings.Replace(html, `<link rel="stylesheet" href="css/main.css">`, `<style>`+string(mainCSS)+`</style>`, 1)
	}

	// Inlined charts.js
	chartsJS, err := ReadFile("js/charts.js")
	if err == nil {
		html = strings.Replace(html, `<script src="js/charts.js"></script>`, `<script>`+string(chartsJS)+`</script>`, 1)
	}

	// Inlined app.js
	appJS, err := ReadFile("js/app.js")
	if err == nil {
		html = strings.Replace(html, `<script src="js/app.js"></script>`, `<script>`+string(appJS)+`</script>`, 1)
	}

	return html, nil
}
