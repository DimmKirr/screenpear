module github.com/automationd/screenpear

go 1.26.0

require (
	github.com/devcell-sh/go-textshot v0.1.0
	github.com/dimmkirr/gowda v0.0.0
	github.com/malcolmston/opencv v0.8.0
	github.com/wailsapp/wails/v3 v3.0.0-alpha2.117
	golang.org/x/image v0.47.0
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/microsoft/onnxruntime/go v0.0.0-20261009200253-58bacbc1485a // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

// Typography API (DetectTypeface) is unreleased on go-textshot main; drop
// this once v0.2.0 is tagged.
replace github.com/devcell-sh/go-textshot => /Users/dmitry/dev/devcell-sh/go-textshot

replace github.com/dimmkirr/gowda => /Users/dmitry/dev/dimmkirr/gowda
