package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// installAppMenu sets the standard role-based menus. Desktop only: Wails
// makes menu calls no-ops on mobile, but skip the work entirely.
func installAppMenu(app *application.App) {
	if !application.System.IsDesktop() {
		return
	}
	isMac := application.System.IsPlatform(application.PlatformMacOS)

	menu := application.NewMenu()
	if isMac {
		appMenu := menu.AddSubmenu(appName)
		appMenu.AddRole(application.About)
		appMenu.AddSeparator()
		appMenu.AddRole(application.ServicesMenu)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Hide)
		appMenu.AddRole(application.HideOthers)
		appMenu.AddRole(application.UnHide)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Quit)
	}

	fileMenu := menu.AddSubmenu("File")
	fileMenu.Add("Show " + appName).
		SetAccelerator("CmdOrCtrl+Shift+S").
		OnClick(func(_ *application.Context) {
			if w := currentMainWindow(); w != nil {
				w.Show()
			}
		})
	fileMenu.AddSeparator()
	if isMac {
		fileMenu.AddRole(application.CloseWindow)
	} else {
		fileMenu.AddRole(application.Quit)
	}

	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)

	app.Menu.SetApplicationMenu(menu)
}

//go:embed build/appicon.png
var appIcon []byte

//go:embed build/tray_icon.png
var trayIcon []byte

// installSystemTray adds the menu-bar / tray item: click toggles the main
// window; the menu offers Show and Quit plus the build version.
func installSystemTray(app *application.App, window mainWindow) {
	if !application.System.IsDesktop() {
		return
	}
	// Linux trays are StatusNotifierItems over D-Bus. Without a session bus
	// the registration panics, so skip it in containers / headless CI, the
	// same way singleInstanceOptions() does.
	if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		log.Println("tray: skipping system tray (no D-Bus session bus)")
		return
	}

	tray := app.SystemTray.New()
	label := fmt.Sprintf("%s %s", appName, version)
	// Windows: NIF_TIP hover text. macOS/Linux: inert.
	tray.SetTooltip(label)
	// macOS menu-bar extras are icon-only; SetLabel there draws literal text
	// beside the icon. On Linux it is the hover title we want.
	if runtime.GOOS != "darwin" {
		tray.SetLabel(label)
	}
	tray.SetIcon(trayIcon)
	tray.OnClick(window.Toggle)

	menu := app.NewMenu()
	menu.Add("Show " + appName).OnClick(func(_ *application.Context) { window.Show() })
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(_ *application.Context) {
		window.Close()
		app.Quit()
	})
	menu.AddSeparator()
	menu.Add("Version: " + version).SetEnabled(false)
	tray.SetMenu(menu)
}
