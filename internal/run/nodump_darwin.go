//go:build darwin

package run

// setNoDump is a no-op on macOS (prctl is Linux-specific).
func setNoDump() {}
