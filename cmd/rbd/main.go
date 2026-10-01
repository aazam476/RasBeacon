// Command rbd is RasBeacon's privileged background program (a "daemon").
//
// It runs as root, started by systemd (image/overlay/usr/lib/systemd/system/
// rbd.service), and owns all network state. The logic lives in
// internal/agent; this file only wires it up:
//
//  1. Set up logging (systemd's journal collects everything on stderr).
//  2. Run the boot sequence (agent.Start).
//  3. Serve the local API that rbweb uses, on /run/rbd/api.sock.
package main

import (
	"flag"
	"log/slog"
	"os"
	"os/user"
	"strconv"

	"rasbeacon/internal/agent"
	"rasbeacon/internal/devsim"
	"rasbeacon/internal/paths"
	"rasbeacon/internal/system"
	"rasbeacon/internal/version"
)

func main() {
	webGroup := flag.String("web-group", "rbweb", "group allowed to use the API socket (rbweb's group)")
	debug := flag.Bool("debug", false, "log every command that is run")
	devRoot := flag.String("dev", "", "run as a simulated device under this directory, without touching "+
		"the real network (for web UI development; see CONTRIBUTING.md)")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	slog.Info("rbd starting", "version", version.Version)

	p := paths.Default()
	var runner system.Runner = system.ExecRunner{}
	if *devRoot != "" {
		var err error
		if p, err = devsim.Setup(*devRoot); err != nil {
			slog.Error("could not create the simulated device", "error", err)
			os.Exit(1)
		}
		runner = devsim.Runner{}
		slog.Warn("SIMULATED DEVICE: no real network changes will be made", "root", *devRoot)
	}
	a := agent.New(p, runner)
	if *devRoot != "" {
		a.SetDHCPProbe(devsim.DHCPProbe)
	}
	if err := a.Start(); err != nil {
		// systemd restarts rbd after a failure (see rbd.service), and the
		// firewall rules loaded at boot stay in place in the meantime.
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	// Watch the internet connection in the background (see agent/uplink.go).
	go a.RunUplinkMonitor()

	if err := a.ListenUnix(p.APISocket(), lookupGroupID(*webGroup)); err != nil {
		slog.Error("API server stopped", "error", err)
		os.Exit(1)
	}
}

// lookupGroupID returns the numeric ID of a group name, or -1 if it does not
// exist (for example when running on a development computer).
func lookupGroupID(name string) int {
	g, err := user.LookupGroup(name)
	if err != nil {
		slog.Warn("group not found; API socket keeps its default group", "group", name)
		return -1
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return -1
	}
	return id
}
