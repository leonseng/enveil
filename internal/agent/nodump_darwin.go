//go:build darwin

package agent

// setNoDump is a no-op on macOS (prctl is Linux-specific).
func setNoDump() {}
