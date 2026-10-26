package plugin

// This file fills in placeholders such as {port:3000} in a plugin's
// manifest (see Values).

import (
	"regexp"
	"strconv"
)

// Values fill in placeholders in a manifest's text (afterInstall, metric
// and health URLs):
//
//	{address}       the plugin's own address, e.g. 10.89.0.2 (for DNS settings)
//	{host}          RasBeacon's address on the hotspot, e.g. 10.87.0.1
//	{port:3000}     the port on RasBeacon the owner chose for container port 3000
//	{setting:key}   a setting's value
type Values struct {
	Address  string
	Host     string
	Ports    map[int]int
	Settings map[string]string
}

// placeholder matches one placeholder, such as {port:3000}.
var placeholder = regexp.MustCompile(`\{(address|host|port:[0-9]{1,5}|setting:[A-Za-z][A-Za-z0-9_]*)\}`)

// Expand fills in the placeholders; unknown ones stay as they are.
func (v Values) Expand(text string) string {
	// ReplaceAllStringFunc calls the function for each match, and puts what
	// it returns in the match's place.
	return placeholder.ReplaceAllStringFunc(text, func(match string) string {
		name := match[1 : len(match)-1] // without the { and }
		switch {
		case name == "address":
			return v.Address
		case name == "host":
			return v.Host
		case len(name) > 5 && name[:5] == "port:":
			container, _ := strconv.Atoi(name[5:])
			if host, ok := v.Ports[container]; ok {
				return strconv.Itoa(host)
			}
		case len(name) > 8 && name[:8] == "setting:":
			if value, ok := v.Settings[name[8:]]; ok {
				return value
			}
		}
		return match
	})
}
