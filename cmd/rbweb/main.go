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
	"path/filepath"
	"time"

	"rasbeacon/internal/paths"
	"rasbeacon/internal/version"
	"rasbeacon/internal/webserver"
	"rasbeacon/web"
)

// main reads the command line and serves the web UI and the API (on HTTP,
// and on HTTPS while it is turned on) until systemd stops it.
func main() {
	listen := flag.String("listen", ":80", "address and port to listen on")
	listenHTTPS := flag.String("listen-https", ":443", "address and port for HTTPS (used when HTTPS is on)")
	socket := flag.String("rbd-socket", paths.Default().APISocket(), "rbd's API socket")
	tlsDir := flag.String("tls-dir", paths.Default().RunTLSDir(), "where rbd puts the HTTPS certificate (cert.pem, key.pem)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	slog.Info("rbweb starting", "version", version.Version, "listen", *listen)

	server := webserver.New(webserver.NewRBDClient(*socket), web.Files())

	// HTTPS is on when rbd has put a certificate here (see agent/https.go).
	// rbweb looks only now, when it starts; rbd restarts it whenever the
	// certificate appears, changes or goes away.
	// rbd restarts rbweb whenever the setting changes.
	certFile := filepath.Join(*tlsDir, "cert.pem")
	keyFile := filepath.Join(*tlsDir, "key.pem")
	if _, err := os.Stat(certFile); err == nil {
		server.UseHTTPS()
		httpsServer := newHTTPServer(*listenHTTPS, server.Handler())
		slog.Info("HTTPS on", "listen", *listenHTTPS)
		go func() {
			if err := httpsServer.ListenAndServeTLS(certFile, keyFile); err != nil {
				slog.Error("HTTPS server stopped", "error", err)
				os.Exit(1)
			}
		}()
	}

	if err := newHTTPServer(*listen, server.Handler()).ListenAndServe(); err != nil {
		slog.Error("web server stopped", "error", err)
		os.Exit(1)
	}
}

// newHTTPServer makes a server with timeouts, which stop slow or stuck
// clients from tying up the server.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
