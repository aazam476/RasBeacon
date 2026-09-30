// Package version holds the RasBeacon version string.
//
// The release build sets it with a linker flag:
//
//	go build -ldflags "-X rasbeacon/internal/version.Version=v0.1.0" ...
//
// Local builds show "dev".
package version

// Version is the running RasBeacon version.
var Version = "dev"
