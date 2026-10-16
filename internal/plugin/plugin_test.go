package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// iconPNG is a real PNG of side×side pixels (plugin icons are checked).
func iconPNG(side int) string {
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, side, side)))
	return b.String()
}

var icon = iconPNG(128)

func validManifest() map[string]any {
	return map[string]any{
		"id": "adguard-home", "name": "AdGuard Home", "version": "0.107.57-1",
		"description": "Blocks ads.",
		"image":       map[string]any{"ref": "docker.io/adguard/adguardhome:v0.107.57", "digest": "sha256:" + strings.Repeat("ab", 32)},
		"permissions": []string{"internet"},
		"ports":       []map[string]any{{"name": "Web interface", "container": 3000, "open": true}},
		"volumes":     []map[string]any{{"name": "conf", "path": "/opt/adguardhome/conf"}},
		"settings":    []map[string]any{{"key": "password", "label": "Password", "type": "password", "required": true}},
		"templates":   []map[string]any{{"file": "AdGuardHome.yaml", "path": "/opt/adguardhome/conf/AdGuardHome.yaml", "once": true}},
		"metrics":     []map[string]any{{"label": "Queries", "url": "http://{address}:3000/control/stats", "path": "num_dns_queries"}},
	}
}

// writeRBA makes a .rba with the given files.
func writeRBA(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.rba")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		fw, _ := w.Create(name)
		fw.Write([]byte(files[name]))
	}
	w.Close()
	f.Close()
	return path
}

func manifestJSON(m map[string]any) string {
	data, _ := json.Marshal(m)
	return string(data)
}

func TestOpen_ReadsAValidPlugin(t *testing.T) {
	path := writeRBA(t, map[string]string{
		ManifestFile: manifestJSON(validManifest()), IconFile: icon, ReadmeFile: "# Hi",
		"templates/AdGuardHome.yaml": "users: []",
	})
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.Manifest.ID != "adguard-home" || a.Readme != "# Hi" || a.PublicKey != nil || a.HasImage() {
		t.Errorf("got %+v", a)
	}
	if a.Manifest.Ports[0].Protocol != "tcp" || a.Manifest.Ports[0].Default != 3000 {
		t.Errorf("port defaults not filled in: %+v", a.Manifest.Ports[0])
	}
	if a.Manifest.Memory() != 256 || a.Manifest.CPULimit() != 0.5 || a.Manifest.Storage() != 256 {
		t.Error("default limits")
	}
}

func TestOpen_RefusesBadFiles(t *testing.T) {
	good := func() map[string]string {
		return map[string]string{ManifestFile: manifestJSON(validManifest()), IconFile: icon, "templates/AdGuardHome.yaml": "x"}
	}
	cases := map[string]func(map[string]string){
		"no manifest":      func(f map[string]string) { delete(f, ManifestFile) },
		"no icon":          func(f map[string]string) { delete(f, IconFile) },
		"icon not png":     func(f map[string]string) { f[IconFile] = "GIF89a" },
		"icon too small":   func(f map[string]string) { f[IconFile] = iconPNG(64) },
		"text after json":  func(f map[string]string) { f[ManifestFile] = manifestJSON(validManifest()) + "{}" },
		"unexpected file":  func(f map[string]string) { f["run.sh"] = "rm -rf /" },
		"path escape":      func(f map[string]string) { f["../evil"] = "x" },
		"missing template": func(f map[string]string) { delete(f, "templates/AdGuardHome.yaml") },
		"image twice":      func(f map[string]string) { f[ImageFile] = "tar" },
		"unknown field":    func(f map[string]string) { m := validManifest(); m["root"] = true; f[ManifestFile] = manifestJSON(m) },
		"bad permission": func(f map[string]string) {
			m := validManifest()
			m["permissions"] = []string{"dns-provider"}
			f[ManifestFile] = manifestJSON(m)
		},
		"bad id": func(f map[string]string) { m := validManifest(); m["id"] = "Bad_ID"; f[ManifestFile] = manifestJSON(m) },
		"bad minRasBeacon": func(f map[string]string) {
			m := validManifest()
			m["minRasBeacon"] = "next year"
			f[ManifestFile] = manifestJSON(m)
		},
		"digest missing": func(f map[string]string) {
			m := validManifest()
			m["image"] = map[string]any{"ref": "docker.io/a/b:1"}
			f[ManifestFile] = manifestJSON(m)
		},
		"template outside": func(f map[string]string) {
			m := validManifest()
			m["templates"] = []map[string]any{{"file": "AdGuardHome.yaml", "path": "/etc/passwd"}}
			f[ManifestFile] = manifestJSON(m)
		},
		"metric elsewhere": func(f map[string]string) {
			m := validManifest()
			m["metrics"] = []map[string]any{{"label": "x", "url": "http://example.com/", "path": "a"}}
			f[ManifestFile] = manifestJSON(m)
		},
		"metric bent elsewhere": func(f map[string]string) {
			m := validManifest()
			m["metrics"] = []map[string]any{{"label": "x", "url": "http://{address}@192.168.1.1/", "path": "a"}}
			f[ManifestFile] = manifestJSON(m)
		},
		"health elsewhere": func(f map[string]string) {
			m := validManifest()
			m["health"] = map[string]any{"url": "http://{address}.example.com/"}
			f[ManifestFile] = manifestJSON(m)
		},
		"port default too big": func(f map[string]string) {
			m := validManifest()
			m["ports"] = []map[string]any{{"name": "Web", "container": 3000, "default": 70000}}
			f[ManifestFile] = manifestJSON(m)
		},
		"image without registry": func(f map[string]string) {
			m := validManifest()
			m["image"] = map[string]any{"ref": "adguard/adguardhome:v1", "digest": "sha256:" + strings.Repeat("ab", 32)}
			f[ManifestFile] = manifestJSON(m)
		},
		"volumes share a path": func(f map[string]string) {
			m := validManifest()
			m["volumes"] = []map[string]any{{"name": "conf", "path": "/opt/adguardhome/conf"}, {"name": "work", "path": "/opt/adguardhome/conf"}}
			f[ManifestFile] = manifestJSON(m)
		},
		"env on two lines": func(f map[string]string) {
			m := validManifest()
			m["env"] = map[string]string{"TZ": "UTC\nEVIL=1"}
			f[ManifestFile] = manifestJSON(m)
		},
		"name on two lines": func(f map[string]string) {
			m := validManifest()
			m["name"] = "Ad\nGuard"
			f[ManifestFile] = manifestJSON(m)
		},
		"options twice": func(f map[string]string) {
			m := validManifest()
			m["settings"] = []map[string]any{{"key": "x", "label": "X", "type": "select",
				"options": []map[string]any{{"value": "a", "label": "A"}, {"value": "a", "label": "Again"}}}}
			f[ManifestFile] = manifestJSON(m)
		},
		"usb no permission": func(f map[string]string) {
			m := validManifest()
			m["usbDrive"] = map[string]any{"path": "/share"}
			f[ManifestFile] = manifestJSON(m)
		},
	}
	for name, change := range cases {
		files := good()
		change(files)
		if _, err := Open(writeRBA(t, files)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSignature_SignedFilesVerifyAndChangesAreCaught(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	src := writeRBA(t, map[string]string{ManifestFile: manifestJSON(validManifest()), IconFile: icon, "templates/AdGuardHome.yaml": "x"})
	signed := filepath.Join(t.TempDir(), "signed.rba")
	if err := Sign(src, signed, private); err != nil {
		t.Fatal(err)
	}
	a, err := Open(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !a.PublicKey.Equal(public) {
		t.Error("the signing key should be reported")
	}
	if KeyID(public) == "" || len(KeyID(public)) != 19 {
		t.Errorf("KeyID = %q", KeyID(public))
	}

	// Copy the signed file with one byte of the readme changed.
	z, _ := zip.OpenReader(signed)
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		files[f.Name] = string(data)
	}
	z.Close()
	files["templates/AdGuardHome.yaml"] = "y"
	if _, err := Open(writeRBA(t, files)); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("a changed file must fail the signature check, got %v", err)
	}

	// Signing a file in place works (the signed copy replaces it).
	if err := Sign(src, src, private); err != nil {
		t.Fatal(err)
	}
	if a, err := Open(src); err != nil || !a.PublicKey.Equal(public) {
		t.Errorf("signed in place: %v", err)
	}
}

func TestSettingCheck(t *testing.T) {
	min, max := 0.0, 100.0
	n := Setting{Key: "speed", Type: "number", Min: &min, Max: &max}
	if n.Check("50") != nil || n.Check("150") == nil || n.Check("x") == nil {
		t.Error("number")
	}
	s := Setting{Type: "select", Options: []Option{{Value: "a"}}}
	if s.Check("a") != nil || s.Check("b") == nil {
		t.Error("select")
	}
	if (Setting{Type: "text", Required: true}).Check("") == nil {
		t.Error("required")
	}
	if (Setting{Type: "text"}).Check("a\nb") == nil {
		t.Error("newlines must be refused (they would break env files)")
	}
}

func TestExpand(t *testing.T) {
	v := Values{Address: "10.89.0.2", Host: "10.87.0.1", Ports: map[int]int{3000: 8080}, Settings: map[string]string{"user": "admin"}}
	got := v.Expand("DNS {address}, open http://{host}:{port:3000} as {setting:user}; {port:9} {unknown}")
	want := "DNS 10.89.0.2, open http://10.87.0.1:8080 as admin; {port:9} {unknown}"
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestReadMetric(t *testing.T) {
	body := []byte(`{"num_dns_queries": 12345, "num_blocked_filtering": 1000, "avg_processing_time": 0.0123, "top": [{"n": "x"}]}`)
	cases := []struct {
		m    Metric
		want string
	}{
		{Metric{Path: "num_dns_queries"}, "12,345"},
		{Metric{Path: "num_blocked_filtering", DivideBy: "num_dns_queries", Format: "percent"}, "8.1%"},
		{Metric{Path: "avg_processing_time", Scale: 1000, Format: "ms"}, "12.3 ms"},
		{Metric{Path: "num_dns_queries", Format: "bytes"}, "12.3 KB"},
		{Metric{Path: "top.0.n", Format: "text"}, "x"},
	}
	for _, c := range cases {
		got, err := ReadMetric(c.m, body)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.m.Path, got, err, c.want)
		}
	}
	if _, err := ReadMetric(Metric{Path: "missing"}, body); err == nil {
		t.Error("a missing value should fail")
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.10.0", "1.9.9", true}, {"v0.107.57-2", "0.107.57-1", true}, {"1.0", "1.0", false},
		{"1.0.1", "1.0", true}, {"1.0", "1.0.1", false}, {"2.0-beta", "2.0-alpha", true},
	}
	for _, c := range cases {
		if got := NewerVersion(c.a, c.b); got != c.want {
			t.Errorf("NewerVersion(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestCheckSource(t *testing.T) {
	for in, want := range map[string]string{
		"aazam476/RasBeacon-plugins": "aazam476/RasBeacon-plugins",
		"https://github.com/a/b.git": "a/b",
	} {
		if got, err := CheckSource(in); err != nil || got != want {
			t.Errorf("%s: %s, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "a", "a/b/c", "../x", "a/.."} {
		if _, err := CheckSource(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSplitRef(t *testing.T) {
	cases := map[string][2]string{
		"docker.io/adguard/adguardhome:v1": {"registry-1.docker.io", "adguard/adguardhome"},
		"docker.io/alpine:3":               {"registry-1.docker.io", "library/alpine"},
		"ghcr.io/owner/image":              {"ghcr.io", "owner/image"},
		"registry.local:5000/a/b:tag":      {"registry.local:5000", "a/b"},
	}
	for ref, want := range cases {
		host, repo := SplitRef(ref)
		if host != want[0] || repo != want[1] {
			t.Errorf("%s: %s %s", ref, host, repo)
		}
	}
}

func TestDownloadSize_LogsInAndPicksArm64(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Write([]byte(`{"token":"t"}`))
		case r.Header.Get("Authorization") != "Bearer t":
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="reg"`)
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasSuffix(r.URL.Path, "/manifests/sha256:index"):
			w.Write([]byte(`{"manifests":[{"digest":"sha256:amd","platform":{"os":"linux","architecture":"amd64"}},{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/manifests/sha256:arm"):
			w.Write([]byte(`{"config":{"size":100},"layers":[{"size":1000},{"size":2000}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	size, err := DownloadSize(srv.Client(), RegistryImage{Ref: host + "/owner/image:1", Digest: "sha256:index"})
	if err != nil || size != 3100 {
		t.Errorf("size = %d, %v", size, err)
	}
}

func TestParseDrives(t *testing.T) {
	out := `{"blockdevices":[
	 {"name":"mmcblk0","path":"/dev/mmcblk0","size":32000000000,"fstype":null,"label":null,"uuid":null,"tran":null,"type":"disk",
	  "children":[{"name":"mmcblk0p2","path":"/dev/mmcblk0p2","size":1,"fstype":"ext4","label":"root","uuid":"r","tran":null,"type":"part"}]},
	 {"name":"sda","path":"/dev/sda","size":64000000000,"fstype":null,"label":null,"uuid":null,"tran":"usb","type":"disk",
	  "children":[{"name":"sda1","path":"/dev/sda1","size":64000000000,"fstype":"exfat","label":"STICK","uuid":"1234-ABCD","tran":null,"type":"part"},
	              {"name":"sda2","path":"/dev/sda2","size":5,"fstype":"swap","label":null,"uuid":"s","tran":null,"type":"part"}]}]}`
	drives, err := ParseDrives(out)
	if err != nil || len(drives) != 1 || drives[0].UUID != "1234-ABCD" || drives[0].Label != "STICK" {
		t.Errorf("drives = %+v, %v (only the USB exfat partition)", drives, err)
	}
	if kind, opts := MountOptions("ntfs"); kind != "ntfs3" || !strings.Contains(opts, "noexec") || !strings.Contains(opts, "nosuid") || !strings.Contains(opts, "nodev") {
		t.Errorf("ntfs mount: %s %s", kind, opts)
	}
}

func TestRunArgs(t *testing.T) {
	args := RunArgs(RunSpec{
		ID: "samba", Image: "localhost/rasbeacon-plugin/samba:1", Address: "10.89.0.3", MemoryMB: 128, CPUs: 0.5,
		Mounts:  []Mount{{Host: "/run/x/data/share", Container: "/share"}},
		Devices: []string{"/dev/i2c-1"}, Env: map[string]string{"B": "2", "A": "1"},
		Masked: []string{"/sys/class/thermal"}, LogFile: "/run/rasbeacon/plugin-logs/samba.log",
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--name rasbeacon-samba", "--network rasbeacon-plugins --ip 10.89.0.3 --dns 10.89.0.1",
		"--memory 128m --cpus 0.5", "--volume /run/x/data/share:/share", "--device /dev/i2c-1",
		"--security-opt mask=/sys/class/thermal", "--env A=1 --env B=2 localhost/rasbeacon-plugin/samba:1",
		"--security-opt no-new-privileges",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "--publish") || strings.Contains(joined, "--privileged") {
		t.Error("ports are RasBeacon's firewall's job, and nothing runs privileged")
	}
	if LocalImage("x", "1.0+b/2") != "localhost/rasbeacon-plugin/x:1.0_b_2" {
		t.Error(LocalImage("x", "1.0+b/2"))
	}
}

func TestSupportedBy(t *testing.T) {
	for _, c := range []struct {
		min, running string
		ok           bool
	}{
		{"", "v0.1.0", true},
		{"0.1.0", "v0.1.0", true},
		{"0.1", "v0.1.0", true},
		{"0.2.0", "v0.1.9", false},
		{"0.2.0", "v0.2.0-beta.1", true},
		{"0.2.1", "v0.2.0-beta.1", false},
		{"1.0", "v0.9.9", false},
		{"0.10.0", "v0.9.0", false},
		{"0.2.0", "dev", true},
		{"0.2.0", "dev-abc1234", true},
	} {
		if got := SupportedBy(c.min, c.running); got != c.ok {
			t.Errorf("SupportedBy(%q, %q) = %v", c.min, c.running, got)
		}
	}
}
