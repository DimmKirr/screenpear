//go:build windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Windows: WebView2 defaults (Evergreen runtime, sandbox on). Dark theme and
// hit-testing support for -webkit-app-region: drag, should the frame ever be
// dropped. No native window: the webview is it.
func applyPlatformAppOptions(_ *application.Options) {}

func applyPlatformWindowOptions(options *application.WebviewWindowOptions) {
	options.Windows = application.WindowsWindow{Theme: application.Dark, NonClientRegionSupport: true}
}

func newNativeMainWindow(_ int) mainWindow { return nil }
