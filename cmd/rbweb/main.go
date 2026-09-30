// Command rbweb is RasBeacon's web server: it serves the web UI and the
// /api/v1/ API to devices on the hotspot.
//
// It runs as the unprivileged "rbweb" user. systemd grants it only the right
// to listen on port 80 (see image/overlay/usr/lib/systemd/system/
// rbweb.service). The logic lives in internal/webserver.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"rasbeacon/internal/paths"
	"rasbeacon/internal/version"
	"rasbeacon/internal/webserver"
	"rasbeacon/web"
)

func main() {
	listen := flag.String("listen", ":80", "address and port to listen on")
	socket := flag.String("rbd-socket", paths.Default().APISocket(), "rbd's API socket")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	slog.Info("rbweb starting", "version", version.Version, "listen", *listen)

	server := webserver.New(webserver.NewRBDClient(*socket), web.Files())
	httpServer := &http.Server{
		Addr:    *listen,
		Handler: server.Handler(),
		// Timeouts stop slow or stuck clients from tying up the server.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		slog.Error("web server stopped", "error", err)
		os.Exit(1)
	}
}
