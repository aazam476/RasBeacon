package agent

// Downloads for plugins, and the catalog. See plugins.go.
//
// Everything RasBeacon downloads for plugins (the catalog, .rba files,
// container images) goes the way devices' traffic does: through the VPN
// when it is in use. RasBeacon's own traffic normally uses the uplink, so
// these connections carry a mark (routing.PluginMark) that a routing rule
// sends into the tunnel, and their names are looked up with RasBeacon's DNS
// server for devices (dnsmasq), which asks the VPN's. Podman downloads
// images through a small proxy here that makes such connections.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
	"rasbeacon/internal/routing"
	"rasbeacon/internal/version"
)

// catalogCache is the catalog as last fetched, saved in PluginCatalogFile.
type catalogCache struct {
	CheckedAt time.Time       `json:"checkedAt"`
	Entries   []catalogRecord `json:"entries"`
	// Errors by source.
	Errors map[string]string `json:"errors,omitempty"`
	// RetryAfter is when GitHub may be asked again after asking to wait.
	RetryAfter time.Time `json:"retryAfter,omitempty"`
	loaded     bool
}

// catalogRecord is a plugin.Entry with its download address kept.
type catalogRecord struct {
	plugin.Entry
	DownloadURL string `json:"url"`
}

// find looks up a plugin by its source and ID (for updates).
func (c *catalogCache) find(source, id string) (catalogRecord, bool) {
	for _, e := range c.Entries {
		if e.Source == source && e.ID == id {
			return e, true
		}
	}
	return catalogRecord{}, false
}

// entry looks up a plugin by its source and file name (for downloads).
func (c *catalogCache) entry(source, file string) (catalogRecord, bool) {
	for _, e := range c.Entries {
		if e.Source == source && e.File == file {
			return e, true
		}
	}
	return catalogRecord{}, false
}

// errorText joins the sources' problems into one line, in a stable order
// (a map's order changes from one loop to the next).
func (c *catalogCache) errorText() string {
	var parts []string
	for _, source := range slices.Sorted(maps.Keys(c.Errors)) {
		parts = append(parts, source+": "+c.Errors[source])
	}
	return strings.Join(parts, "; ")
}

// pluginDialer makes connections for plugins: marked for the tunnel, with
// names looked up by RasBeacon's DNS server for devices.
//
// The mark is a number the kernel attaches to each packet of a connection
// (SO_MARK). A routing rule (routing.PluginMark) sends marked packets the
// way devices' traffic goes: into the VPN tunnel when it is in use.
// Without the mark, RasBeacon's own connections would leave through the
// uplink, outside the VPN.
func (a *Agent) pluginDialer() *net.Dialer {
	a.mu.Lock()
	dns := net.JoinHostPort(a.lan.Address(), "53")
	a.mu.Unlock()
	mark, _ := strconv.ParseUint(strings.TrimPrefix(routing.PluginMark, "0x"), 16, 32)
	return &net.Dialer{
		Timeout: 20 * time.Second,
		// Control runs on each new socket before it connects: the place to
		// set socket options such as the mark.
		Control: func(network, address string, c syscall.RawConn) error {
			var markErr error
			err := c.Control(func(fd uintptr) {
				markErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, int(mark))
			})
			if err != nil {
				return err
			}
			return markErr
		},
		// Names are looked up with dnsmasq on RasBeacon's hotspot address,
		// which asks the VPN's DNS servers when the VPN is in use (the
		// uplink's resolver would ask outside the tunnel).
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, dns)
			},
		},
	}
}

// pluginHTTPClient is the HTTP client for plugin downloads.
func (a *Agent) pluginHTTPClient() *http.Client {
	dialer := a.pluginDialer()
	return &http.Client{
		Timeout: 30 * time.Minute,
		Transport: &http.Transport{
			Proxy:               nil, // never a proxy from the environment
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 20 * time.Second,
			IdleConnTimeout:     30 * time.Second,
		},
	}
}

// downloadProxy starts (once) the proxy Podman downloads images through,
// on 127.0.0.1, and returns its address. It only makes HTTPS connections
// (CONNECT to port 443), marked like the other plugin downloads. (Podman is
// a separate program, so it can't use pluginDialer; it can use a proxy,
// given in its HTTPS_PROXY environment variable.)
func (a *Agent) downloadProxy() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.plugins.proxy != "" {
		return a.plugins.proxy, nil
	}
	// Port 0: the system picks a free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	a.plugins.proxy = listener.Addr().String()
	server := &http.Server{
		Handler:           http.HandlerFunc(a.proxyConnect),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	return a.plugins.proxy, nil
}

// proxyConnect handles one CONNECT request: "connect me to host:443".
func (a *Agent) proxyConnect(w http.ResponseWriter, r *http.Request) {
	_, port, err := net.SplitHostPort(r.Host)
	if r.Method != http.MethodConnect || err != nil || port != "443" {
		http.Error(w, "only HTTPS connections", http.StatusForbidden)
		return
	}
	connectTunnel(w, r, func(ctx context.Context) (net.Conn, error) {
		return a.pluginDialer().DialContext(ctx, "tcp", r.Host)
	})
}

// connectTunnel answers a CONNECT request: it opens the connection dial
// makes, tells the client it is open, and then copies bytes both ways
// until both sides are done. What goes through is encrypted (HTTPS), so
// the proxy never sees it.
func connectTunnel(w http.ResponseWriter, r *http.Request, dial func(context.Context) (net.Conn, error)) {
	upstream, err := dial(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Hijack takes the connection over from Go's HTTP server, so it can be
	// used as a plain two-way stream.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "can't proxy", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	// The answer is written by hand: after Hijack, the HTTP server no longer
	// writes anything (and an answer it wrote could add headers, such as
	// "Transfer-Encoding: chunked", that don't belong in a tunnel).
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
		client.Close()
		upstream.Close()
		return
	}
	// The client may have sent bytes after its request already; the HTTP
	// server read them into its buffer, so pass them on first.
	if n := buffered.Reader.Buffered(); n > 0 {
		data, _ := buffered.Reader.Peek(n)
		_, _ = upstream.Write(data)
	}
	// Two goroutines copy the two directions at the same time; the
	// WaitGroup waits for both to finish.
	var wg sync.WaitGroup
	wg.Add(2)
	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		// src ended: tell dst nothing more comes, without closing the
		// other direction.
		if tcp, ok := dst.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}
	go pipe(upstream, client)
	go pipe(client, upstream)
	wg.Wait()
	client.Close()
	upstream.Close()
}

// --- The catalog -------------------------------------------------------

// loadCatalog reads the saved catalog once. The caller holds a.mu.
func (a *Agent) loadCatalog() {
	if a.plugins.catalog.loaded {
		return
	}
	a.plugins.catalog.loaded = true
	if err := config.ReadJSON(a.paths.PluginCatalogFile(), &a.plugins.catalog); err != nil && !errors.Is(err, config.ErrNotFound) {
		slog.Warn("could not read the saved catalog", "error", err)
	}
}

// PluginCatalog lists the plugins the sources offer. GitHub is only asked
// when refresh is set (Check now), or nothing was fetched yet.
func (a *Agent) PluginCatalog(req api.PluginCatalogRequest) api.PluginCatalog {
	a.mu.Lock()
	a.loadCatalog()
	fetch := req.Refresh || a.plugins.catalog.CheckedAt.IsZero()
	a.mu.Unlock()
	if fetch {
		a.refreshCatalog()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	board := a.pluginBoard()
	result := api.PluginCatalog{Entries: []api.PluginCatalogEntry{}, Errors: a.plugins.catalog.Errors}
	if !a.plugins.catalog.CheckedAt.IsZero() {
		result.CheckedAt = a.plugins.catalog.CheckedAt.UTC().Format(time.RFC3339)
	}
	for _, e := range a.plugins.catalog.Entries {
		entry := api.PluginCatalogEntry{
			Source: e.Source, File: e.File, ID: e.ID, Name: e.Name, Version: e.Version, Description: e.Description,
			Permissions: e.Permissions, Size: e.Size, DownloadSize: e.DownloadSize, Icon: e.Icon,
			RunsHere: (&plugin.Manifest{Boards: e.Boards}).RunsOn(board),
		}
		if !plugin.SupportedBy(e.MinRasBeacon, version.Version) {
			entry.NeedsRasBeacon = e.MinRasBeacon
		}
		if p, ok := a.cfg.Plugins.Installed[e.ID]; ok && e.ID != "" {
			entry.InstalledVersion = p.Version
		}
		result.Entries = append(result.Entries, entry)
	}
	return result
}

// refreshCatalog asks every source for its newest plugins (through the
// VPN), unless GitHub asked to wait. Called without a.mu.
func (a *Agent) refreshCatalog() {
	a.mu.Lock()
	a.loadCatalog()
	if a.now().Before(a.plugins.catalog.RetryAfter) {
		a.mu.Unlock()
		return
	}
	var sources []string
	if !a.cfg.Plugins.NoBuiltInSource {
		sources = append(sources, plugin.BuiltInSource)
	}
	sources = append(sources, a.cfg.Plugins.Sources...)
	a.mu.Unlock()

	blocked := a.waitForPluginPath("")
	fresh := catalogCache{CheckedAt: a.now(), Errors: map[string]string{}, loaded: true}
	client := a.plugins.client()
	for _, source := range sources {
		if blocked != nil {
			fresh.Errors[source] = sentence(blocked.Error())
			continue
		}
		entries, err := plugin.FetchSource(client, source)
		var limited *plugin.RateLimited
		if errors.As(err, &limited) {
			fresh.RetryAfter = limited.Until
		}
		if err != nil {
			fresh.Errors[source] = sentence(err.Error())
			continue
		}
		for _, e := range entries {
			fresh.Entries = append(fresh.Entries, catalogRecord{Entry: e, DownloadURL: e.URL()})
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	// A source that failed keeps what it offered last time.
	for _, old := range a.plugins.catalog.Entries {
		if _, failed := fresh.Errors[old.Source]; failed {
			fresh.Entries = append(fresh.Entries, old)
		}
	}
	a.plugins.catalog = fresh
	if err := config.WriteJSON(a.paths.PluginCatalogFile(), fresh, 0o600); err != nil {
		slog.Warn("could not save the catalog", "error", err)
	}
}

// dailyCatalogCheck looks for updates once a day, but only once a plugin
// from the catalog is installed (GitHub is otherwise only asked when the
// owner opens the catalog). Updates are never installed by themselves.
func (a *Agent) dailyCatalogCheck() {
	if a.catalogCheckDue() {
		slog.Info("checking the plugin catalog for updates")
		a.refreshCatalog()
	}
}

// catalogCheckDue reports whether the daily catalog check should ask
// GitHub: a plugin from the catalog is installed, and the last check is
// more than a day ago.
func (a *Agent) catalogCheckDue() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.loadCatalog()
	due := a.now().Sub(a.plugins.catalog.CheckedAt) > 24*time.Hour
	for _, p := range a.cfg.Plugins.Installed {
		if p.Source != "file" {
			return due
		}
	}
	return false
}
