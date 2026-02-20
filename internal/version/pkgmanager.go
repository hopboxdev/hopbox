package version

import "strings"

// DetectPackageManager returns the package manager name if hop was installed
// via one, or empty string if standalone. Checks the build-time PackageManager
// var first, then falls back to executable path heuristics.
func DetectPackageManager(execPath string) string {
	if PackageManager != "" {
		return PackageManager
	}
	if strings.Contains(execPath, "/Cellar/") ||
		strings.Contains(execPath, "/homebrew/") ||
		strings.Contains(execPath, "linuxbrew/") {
		return "brew"
	}
	if strings.Contains(execPath, "/nix/store/") {
		return "nix"
	}
	return ""
}
