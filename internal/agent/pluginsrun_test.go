package agent

import (
	"slices"
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
		case "html":
			calls = append(calls, `{{html "<b>"}}`)
		default:
			t.Fatalf("this test doesn't know %q yet: add a call to it", name)
		}
	}
	values := plugin.Values{Ports: map[int]int{3000: 3001}}
	out, err := fillTemplate("config", []byte(strings.Join(calls, " ")), values)
	if err != nil || !strings.Contains(string(out), "3001") || !strings.Contains(string(out), `"x"`) || !strings.Contains(string(out), "&lt;b&gt;") {
		t.Errorf("fillTemplate = %q, %v", out, err)
	}
}

// json and html can each make a text longer, so nesting them could grow
// it at every level: a result larger than a whole template is refused,
// before it is written anywhere.
func TestFillTemplate_RefusesHugeValues(t *testing.T) {
	quotes := plugin.Values{Settings: map[string]string{"x": strings.Repeat(`"`, 1<<20)}}
	for _, text := range []string{
		"{{json (json (json .Settings.x))}}", // each json doubles the quotes
		"{{html (html .Settings.x)}}",        // " becomes &#34;, then &amp;#34;
	} {
		if out, err := fillTemplate("config", []byte(text), quotes); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("%s = %d bytes, %v; want refused as too large", text, len(out), err)
		}
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

// commandIndex is where the first command starting with prefix is in the
// list (-1 if it never ran).
func commandIndex(commands []string, prefix string) int {
	for i, c := range commands {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

func TestPluginNetwork_PodmanNetworkIsMadeBeforeTheBridgeGetsItsAddress(t *testing.T) {
	e := newPluginEnv(t)
	ensure := func() {
		e.runner.commands = nil
		e.agent.mu.Lock()
		e.agent.ensurePluginNetwork()
		e.agent.mu.Unlock()
	}

	// Not made yet, and the bridge already has its address (rbd restarted
	// without a reboot): Podman would refuse the subnet, so the address is
	// taken off first and put back after.
	e.runner.failing = append(e.runner.failing, "podman network exists")
	e.runner.outputs["ip link show dev rbplug0"] = "5: rbplug0: <BROADCAST,UP> mtu 1500\n"
	ensure()
	flush := commandIndex(e.runner.commands, "ip address flush dev rbplug0")
	create := commandIndex(e.runner.commands, "podman network create")
	address := commandIndex(e.runner.commands, "ip address replace 10.89.0.1/24 dev rbplug0")
	if flush < 0 || create < flush || address < create {
		t.Errorf("want flush, then create, then the address; commands: %v", e.runner.commands)
	}

	// Made before (Podman keeps it in /persistent): nothing to do.
	e.runner.failing = slices.DeleteFunc(e.runner.failing, func(p string) bool { return p == "podman network exists" })
	ensure()
	if e.runner.ran("podman network create") || e.runner.ran("ip address flush") {
		t.Errorf("an existing network must be left alone; commands: %v", e.runner.commands)
	}
}
