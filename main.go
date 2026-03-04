package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	// Register all database drivers
	_ "warehouse-ui/internal/driver"

	// Register all AI providers
	_ "warehouse-ui/internal/ai"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// TODO: Add "serve" subcommand for HTTP server mode
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		fmt.Println("Server mode not yet implemented. Use desktop mode for now.")
		os.Exit(1)
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Warehouse UI",
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 680,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 15, B: 20, A: 1},
		OnStartup:        app.startup,
		OnShutdown:        app.shutdown,
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                 false,
				HideTitleBar:              false,
				FullSizeContent:           true,
			},
			Appearance: mac.NSAppearanceNameDarkAqua,
			About: &mac.AboutInfo{
				Title:   "Warehouse UI",
				Message: "Open-source universal database IDE\nhttps://github.com/olegnazarov/warehouse-ui",
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
