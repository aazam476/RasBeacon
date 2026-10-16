// Command rbdoh is RasBeacon's DNS-over-HTTPS forwarder: dnsmasq passes
// devices' DNS questions to it on 127.0.0.1:5053, and it asks the owner's
// DoH servers over HTTPS. The logic lives in internal/doh.
//
// rbd starts it (rasbeacon-doh.service) only while DoH servers are chosen
// and in use, with them in /run/rbd/doh.env:
//
//	RBDOH_SERVERS   the DoH servers' URLs, space-separated
//	RBDOH_BOOTSTRAP plain DNS servers that look up the DoH servers' names,
//	                space-separated, asked in turn
//
// systemd runs it as a throwaway unprivileged user (DynamicUser).
package main

import (
	"crypto/x509"
	"flag"
	"log/slog"
	"net"
	"os"
	"strings"

	"rasbeacon/internal/doh"
	"rasbeacon/internal/version"
)

// main reads the servers from the environment and answers DNS questions on
// the listen address until systemd stops it.
func main() {
	listen := flag.String("listen", "127.0.0.1:5053", "address and port to answer DNS on (UDP and TCP)")
	caFile := flag.String("ca", "", "trust only the certificate authorities in this PEM file (tests)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	servers := strings.Fields(os.Getenv("RBDOH_SERVERS"))
	bootstrap := strings.Fields(os.Getenv("RBDOH_BOOTSTRAP"))
	slog.Info("rbdoh starting", "version", version.Version, "listen", *listen, "servers", servers, "bootstrap", bootstrap)

	options := doh.Options{Servers: servers, Bootstrap: bootstrap}
	if *caFile != "" {
		pem, err := os.ReadFile(*caFile)
		options.RootCAs = x509.NewCertPool()
		if err != nil || !options.RootCAs.AppendCertsFromPEM(pem) {
			slog.Error("rbdoh can't read the certificate authorities", "file", *caFile, "error", err)
			os.Exit(1)
		}
	}
	f, err := doh.New(options)
	if err != nil {
		slog.Error("rbdoh can't start", "error", err)
		os.Exit(1)
	}
	udp, err := net.ListenPacket("udp", *listen)
	if err != nil {
		slog.Error("rbdoh can't listen", "error", err)
		os.Exit(1)
	}
	tcp, err := net.Listen("tcp", *listen)
	if err != nil {
		slog.Error("rbdoh can't listen", "error", err)
		os.Exit(1)
	}
	go func() {
		slog.Error("TCP stopped", "error", f.ServeTCP(tcp))
		os.Exit(1)
	}()
	slog.Error("UDP stopped", "error", f.ServeUDP(udp))
	os.Exit(1)
}
