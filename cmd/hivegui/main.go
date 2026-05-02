// hivegui is the desktop client for hived. See
// docs/native-rewrite/phase-1.md for its role in the native rewrite.
package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// resolveLaunchDir is best-effort capture of the user's intent for
// "where should new sessions open". macOS launches .app bundles with
// cwd "/" regardless of how they were invoked (open, Finder, even
// running Contents/MacOS/<bin> directly), so os.Getwd alone is not
// reliable. We try, in order:
//  1. os.Getwd() if it isn't "" or "/"
//  2. $PWD env var (preserved when the user ran the binary directly
//     from a shell that exports PWD)
//  3. $HOME
func resolveLaunchDir() string {
	if cwd, err := os.Getwd(); err == nil && cwd != "" && cwd != "/" {
		return cwd
	}
	if pwd := os.Getenv("PWD"); pwd != "" && pwd != "/" {
		return pwd
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

func main() {
	launchDir := resolveLaunchDir()
	app := NewApp(launchDir)

	// Restore the previous window geometry. Width/Height are passed
	// at construction; the position is applied in OnStartup once we
	// have the Wails runtime context.
	width, height := 1024, 700
	if g, ok := loadWindowGeometry(); ok {
		width, height = g.W, g.H
		app.initialX, app.initialY = g.X, g.Y
		app.haveInitialPos = true
	}

	err := wails.Run(&options.App{
		Title:            "Hive",
		Width:            width,
		Height:           height,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		Menu:             buildAppMenu(app),
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		println("hivegui:", err.Error())
	}
}
