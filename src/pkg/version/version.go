package version

// version is injected at build time via:
//   go build -ldflags="-X git-mcp/pkg/version.version=1.0.1"
var version = "dev"

// Set sets the version string
func Set(v string) {
	if v != "" {
		version = v
	}
}

// Get returns the current version string
func Get() string {
	return version
}
