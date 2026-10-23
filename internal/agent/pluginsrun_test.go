package agent

import (
	"strings"
	"testing"

	"rasbeacon/internal/plugin"
	"rasbeacon/internal/render"
)

// Filling in a plugin's templates (fillTemplate in pluginsrun.go).

// Every function plugin.CheckTemplate allows is one fillTemplate defines.
func TestFillTemplate_DefinesEveryAllowedFunction(t *testing.T) {
	var calls []string
	for _, name := range plugin.TemplateFuncNames {
		switch name {
		case "bcrypt":
			calls = append(calls, `{{bcrypt "secret"}}`)
		case "json":
			calls = append(calls, `{{json "x"}}`)
		case "port":
			calls = append(calls, `{{port 3000}}`)
		default:
			t.Fatalf("this test doesn't know %q yet: add a call to it", name)
		}
	}
	values := plugin.Values{Ports: map[int]int{3000: 3001}}
	out, err := fillTemplate("config", []byte(strings.Join(calls, " ")), values)
	if err != nil || !strings.Contains(string(out), "3001") || !strings.Contains(string(out), `"x"`) {
		t.Errorf("fillTemplate = %q, %v", out, err)
	}
}

// A plugin installed before the check existed is checked when it starts.
func TestFillTemplate_RefusesLoops(t *testing.T) {
	if _, err := fillTemplate("config", []byte("{{range 1000000000000}}x{{end}}"), plugin.Values{}); err == nil || !strings.Contains(err.Error(), "uses range") {
		t.Errorf("fillTemplate = %v, want refused", err)
	}
}

// The firewall (internal/render) names the plugins' bridge and ports
// without importing the plugin package, so the two must agree.
func TestPluginNetwork_NamesMatchTheFirewall(t *testing.T) {
	if render.PluginBridge != plugin.NetworkInterface || render.PluginGateway != plugin.Gateway {
		t.Errorf("bridge %s/%s, want %s/%s", render.PluginBridge, render.PluginGateway, plugin.NetworkInterface, plugin.Gateway)
	}
	if port := plugin.HostPortName("10.89.0.17"); port != "rbport17" || !strings.HasPrefix(port, render.PluginPortPrefix) {
		t.Errorf("port name %q doesn't start with the firewall's %q", port, render.PluginPortPrefix)
	}
}
