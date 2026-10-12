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
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

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
	devHotel := flag.String("dev-hotel", "", "with -dev: run a simulated hotel login page on this address (e.g. 127.0.0.1:8099)")
	devBrowser := flag.String("dev-browser", "", "with -dev: the headless Chromium to use as the login browser")
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
		// The simulated device runs the browser as the current user, and
		// without Chromium's sandbox, which many development machines
		// and CI runners can't provide.
		a.SetBrowser(*devBrowser, true)
		// The speed test runs against a stand-in for Cloudflare's service.
		server, err := devsim.StartSpeedtestServer()
		if err != nil {
			slog.Error("could not start the simulated speed test service", "error", err)
			os.Exit(1)
		}
		a.SetSpeedtestServer(server)
		if *devHotel != "" {
			if err := devsim.StartHotel(*devHotel); err != nil {
				slog.Error("could not start the simulated hotel", "error", err)
				os.Exit(1)
			}
		}
	}
	a.SetWebGroup(lookupGroupID(*webGroup))
	if err := a.Start(); err != nil {
		// systemd restarts rbd after a failure (see rbd.service), and the
		// firewall rules loaded at boot stay in place in the meantime.
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	// Watch the internet connection in the background (see agent/uplink.go).
	go a.RunUplinkMonitor()
	if *devRoot == "" {
		// Keep the clock right (see agent/clock.go). These two are not run
		// on the simulated device: they would contact real web servers.
		go a.RunClockMonitor()
		// Watch for hotel login pages (see agent/portal.go).
		go a.RunPortalMonitor(agent.ProbeInternet)
		// The optional check for new releases (off unless turned on).
		go a.RunUpdateCheck()
		// The optional factory-reset button (see internal/button).
		go a.RunResetButton()
	}
	if *devRoot != "" {
		go a.RunPortalMonitor(devsim.PortalProbe)
	}
	// When the device shuts down, save the time for the next boot.
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
		<-stop
		a.SaveClock()
		slog.Info("rbd stopping")
		os.Exit(0)
	}()

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
