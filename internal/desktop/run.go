package desktop

import (
	"context"
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// Run opens the window with the web interface from assets and blocks until it closes.
func Run(a *App, assets fs.FS) error {
	return wails.Run(&options.App{
		Title:            "Lman",
		Width:            1180,
		Height:           780,
		MinWidth:         360,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 246, G: 246, B: 243, A: 255},
		OnStartup: func(context.Context) {
			applyAppIcon(appIcon)
		},
		OnShutdown: func(context.Context) {
			if err := a.close(); err != nil {
				log.Printf("close connection: %v", err)
			}
		},
		Bind: []any{a},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "Lman",
				Message: "Programming languages on your computer",
				Icon:    appIcon,
			},
		},
		Linux: &linux.Options{
			Icon:             appIcon,
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
	})
}
