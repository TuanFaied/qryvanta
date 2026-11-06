// Command qryvanta is the Qryvanta desktop application.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/TuanFaied/qryvanta/frontend"
	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/desktop"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	core, err := app.New(app.Options{
		// QRYVANTA_CONFIG points the app at another config file (and the connections file next
		// to it), for development and manual testing without touching real settings.
		ConfigPath:           os.Getenv("QRYVANTA_CONFIG"),
		RecoverInvalidConfig: true,
	})
	if err != nil {
		return fmt.Errorf("start qryvanta: %w", err)
	}
	defer core.Close()

	wailsApp := application.New(application.Options{
		Name:        "Qryvanta",
		Description: "Desktop SQL execution debugging platform",
		Logger:      core.Logger(),
		Services: []application.Service{
			application.NewService(desktop.NewAppService(core)),
			application.NewService(desktop.NewConnectionService(core)),
			application.NewService(desktop.NewSchemaService(core)),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(frontend.Assets),
			Middleware: desktop.SecurityHeaders(contentSecurityPolicy()),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Qryvanta",
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 640,
		// Matches the dark theme background so there is no white flash on startup.
		BackgroundColour: application.NewRGB(14, 15, 19),
		URL:              "/",
	})

	if err := wailsApp.Run(); err != nil {
		core.Logger().Error("application exited with error", "err", err)
		return err
	}
	return nil
}
