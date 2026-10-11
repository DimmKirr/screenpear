package main

import (
	"errors"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ShellService is the small set of OS integrations the webview cannot do on
// its own: native file dialogs and revealing results in the file manager.
// Bound into the webview; the SwiftUI window uses NSOpenPanel directly.
type ShellService struct{}

// NewShellService constructs the service.
func NewShellService() *ShellService { return &ShellService{} }

// PickImage opens the native file chooser filtered to images and returns the
// absolute path, or "" when the user cancels.
func (s *ShellService) PickImage() (string, error) {
	dialog := application.Get().Dialog.OpenFile().
		SetTitle("Choose a screenshot").
		AddFilter("Images", "*.png;*.jpg;*.jpeg;*.gif").
		CanChooseFiles(true)
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	return filepath.Abs(path)
}

// Reveal opens the given file's folder in the platform file manager.
func (s *ShellService) Reveal(path string) error {
	if path == "" {
		return errors.New("path is required")
	}
	return application.Get().Browser.OpenFile(filepath.Dir(path))
}
