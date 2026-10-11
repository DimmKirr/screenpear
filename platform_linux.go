//go:build !darwin && !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Linux: WebKitGTK is a link-time dependency, nothing to locate at runtime.
// The window gets an empty menu so it does not inherit the application menu
// bar (File/Edit/View) above the page. No native window: the webview is it.
func applyPlatformAppOptions(_ *application.Options) {}

func applyPlatformWindowOptions(options *application.WebviewWindowOptions) {
	options.Linux = application.LinuxWindow{Menu: application.NewMenu()}
}

func newNativeMainWindow(_ int) mainWindow { return nil }
