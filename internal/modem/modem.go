// Package modem reads what ModemManager says about mobile broadband modems
// (4G/5G modems that need a modem manager: QMI and MBIM), through its
// command-line tool's JSON output (`mmcli -J`). rbd decides what to do with
// it (internal/agent/modem.go); this package only reads.
//
// mmcli writes every value as text ("75", "yes"), and a missing value as
// "--", so the readers below turn those into numbers, booleans and "".
package modem

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Modem is one modem, from `mmcli -m <n> -J`.
type Modem struct {
	// Index is the modem's number for mmcli -m ("0").
	Index               string
	Manufacturer, Model string
	// State is ModemManager's: "locked" (the SIM needs its PIN),
	// "disabled", "enabling", "enabled", "searching", "registered",
	// "connecting", "connected", "failed" and a few more.
	State string
	// FailedReason says why when State is "failed" ("sim-missing").
	FailedReason string
	// UnlockRequired is "sim-pin", "sim-puk" or "" (nothing to unlock).
	UnlockRequired string
	// PINTries is how many PIN attempts the SIM has left (-1: unknown).
	PINTries int
	// SignalPercent is the signal quality, 0 to 100.
	SignalPercent int
	// AccessTech is the network type in use ("lte", "5gnr", "umts"...).
	AccessTech []string
	Operator   string
	// SIM and Bearers are the paths' last numbers, for mmcli -i and -b.
	SIM     string
	Bearers []string
	// DataPort is the network interface it carries data on ("wwan0").
	DataPort string
}

// Bearer is a modem's data connection, from `mmcli -b <n> -J`.
type Bearer struct {
	Connected bool
	Interface string
	// IPv4 is how to set up the interface: "static" (Address, Gateway and
	// DNS are given) or "dhcp".
	Method  string
	Address netip.Prefix
	Gateway netip.Addr
	DNS     []netip.Addr
	MTU     int
}

// List reads `mmcli -L -J`: the modems' numbers.
func List(out string) ([]string, error) {
	var list struct {
		Modems []string `json:"modem-list"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("reading the modem list: %w", err)
	}
	var indexes []string
	for _, p := range list.Modems {
		indexes = append(indexes, path.Base(p))
	}
	return indexes, nil
}

// text turns mmcli's "--" (nothing) into "".
func text(s string) string {
	if s == "--" {
		return ""
	}
	return s
}

// number reads a number mmcli wrote as text, or -1.
func number(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(text(s)))
	if err != nil {
		return -1
	}
	return n
}

// retries finds "sim-pin (3)" in mmcli's unlock retries.
var retries = regexp.MustCompile(`^sim-pin \((\d+)\)$`)

// Parse reads `mmcli -m <n> -J`.
func Parse(out string) (Modem, error) {
	var doc struct {
		Modem struct {
			DBusPath string `json:"dbus-path"`
			Generic  struct {
				Manufacturer   string   `json:"manufacturer"`
				Model          string   `json:"model"`
				State          string   `json:"state"`
				FailedReason   string   `json:"state-failed-reason"`
				UnlockRequired string   `json:"unlock-required"`
				UnlockRetries  []string `json:"unlock-retries"`
				Signal         struct {
					Value string `json:"value"`
				} `json:"signal-quality"`
				AccessTech []string `json:"access-technologies"`
				SIM        string   `json:"sim"`
				Bearers    []string `json:"bearers"`
				Ports      []string `json:"ports"`
			} `json:"generic"`
			ThreeGPP struct {
				Operator string `json:"operator-name"`
			} `json:"3gpp"`
		} `json:"modem"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return Modem{}, fmt.Errorf("reading the modem: %w", err)
	}
	g := doc.Modem.Generic
	m := Modem{
		Index:          path.Base(doc.Modem.DBusPath),
		Manufacturer:   text(g.Manufacturer),
		Model:          text(g.Model),
		State:          text(g.State),
		FailedReason:   text(g.FailedReason),
		UnlockRequired: text(g.UnlockRequired),
		PINTries:       -1,
		SignalPercent:  max(0, number(g.Signal.Value)),
		Operator:       text(doc.Modem.ThreeGPP.Operator),
	}
	if m.UnlockRequired == "none" {
		m.UnlockRequired = ""
	}
	for _, r := range g.UnlockRetries {
		if match := retries.FindStringSubmatch(strings.TrimSpace(r)); match != nil {
			m.PINTries, _ = strconv.Atoi(match[1])
		}
	}
	for _, tech := range g.AccessTech {
		if t := text(tech); t != "" {
			m.AccessTech = append(m.AccessTech, t)
		}
	}
	if sim := text(g.SIM); sim != "" && sim != "/" {
		m.SIM = path.Base(sim)
	}
	for _, b := range g.Bearers {
		m.Bearers = append(m.Bearers, path.Base(b))
	}
	// Ports look like "wwan0 (net)".
	for _, p := range g.Ports {
		if name, kind, ok := strings.Cut(p, " "); ok && kind == "(net)" {
			m.DataPort = name
		}
	}
	return m, nil
}

// ParseBearer reads `mmcli -b <n> -J`.
func ParseBearer(out string) (Bearer, error) {
	var doc struct {
		Bearer struct {
			Status struct {
				Connected string `json:"connected"`
				Interface string `json:"interface"`
			} `json:"status"`
			IPv4 struct {
				Method  string   `json:"method"`
				Address string   `json:"address"`
				Prefix  string   `json:"prefix"`
				Gateway string   `json:"gateway"`
				DNS     []string `json:"dns"`
				MTU     string   `json:"mtu"`
			} `json:"ipv4-config"`
		} `json:"bearer"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return Bearer{}, fmt.Errorf("reading the modem's connection: %w", err)
	}
	b := doc.Bearer
	bearer := Bearer{
		Connected: b.Status.Connected == "yes",
		Interface: text(b.Status.Interface),
		Method:    text(b.IPv4.Method),
		MTU:       max(0, number(b.IPv4.MTU)),
	}
	if addr, err := netip.ParseAddr(text(b.IPv4.Address)); err == nil {
		if bits := number(b.IPv4.Prefix); bits >= 0 && bits <= 32 {
			bearer.Address = netip.PrefixFrom(addr, bits)
		}
	}
	if gw, err := netip.ParseAddr(text(b.IPv4.Gateway)); err == nil {
		bearer.Gateway = gw
	}
	for _, d := range b.IPv4.DNS {
		if addr, err := netip.ParseAddr(text(d)); err == nil {
			bearer.DNS = append(bearer.DNS, addr)
		}
	}
	return bearer, nil
}

// Name is how the pages name a modem: "Quectel EM12-G", or "Mobile modem".
func (m Modem) Name() string {
	name := strings.TrimSpace(m.Manufacturer + " " + m.Model)
	if name == "" {
		return "Mobile modem"
	}
	return name
}

// Network is the network type in words: "5G", "4G", "3G" or "".
func (m Modem) Network() string {
	best := ""
	for _, tech := range m.AccessTech {
		switch {
		case strings.HasPrefix(tech, "5g"):
			return "5G"
		case tech == "lte":
			best = "4G"
		case best == "" && (tech == "umts" || strings.HasPrefix(tech, "hspa") || strings.HasPrefix(tech, "hsdpa") || strings.HasPrefix(tech, "hsupa")):
			best = "3G"
		}
	}
	return best
}
