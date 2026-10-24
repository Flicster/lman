//go:build !darwin

package desktop

// applyAppIcon is the Dock icon on macOS. Other systems take the icon from the window options.
func applyAppIcon([]byte) {}
