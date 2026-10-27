package plugin

import (
	"strings"
	"testing"
)

func TestCheckTemplate(t *testing.T) {
	tests := []struct {
		name, text, wantError string
	}{
		{"the settings and functions", `user: {{json .Settings.username}}
password: {{json (bcrypt .Settings.password)}}
port: {{port 3000}} {{if .Settings.on}}on{{else}}off{{end}} {{with .Address}}{{.}}{{end}}`, ""},
		{"an empty file", "", ""},
		{"a loop", "{{range 1000000000000}}x{{end}}", "uses range"},
		{"a loop inside an if", "{{if .Settings.on}}{{range .Settings.list}}x{{end}}{{end}}", "uses range"},
		{"a loop in an else", "{{with .Address}}x{{else}}{{range 5}}{{end}}{{end}}", "uses range"},
		{"templates calling each other", `{{define "a"}}x{{end}}{{template "a"}}`, "uses define or block"},
		{"a block", `{{block "a" .}}x{{end}}`, "uses define or block"},
		{"a template call", `{{template "missing"}}`, "uses template"},
		{"an unknown function", "{{exec .Settings.x}}", "is broken"},
		{"comparisons", `{{if eq .Settings.mode "fast"}}x{{else if not .Settings.on}}y{{end}} {{$.Address}}`, ""},
		// Each line doubles $a: 20 lines are 4 MB, 30 lines 4 GB.
		{"a variable that doubles", "{{$a := \"xx\"}}" + strings.Repeat("{{$a = json $a}}", 30) + "{{$a}}", "uses variables"},
		{"a variable in an if", "{{if $a := .Settings.x}}{{$a}}{{end}}", "uses variables"},
		{"printf", `{{printf "%999999999d" 1}}`, "uses printf"},
		{"printf in parentheses", `{{json (printf "%999999999d" 1)}}`, "uses printf"},
		{"printf in a with", `{{with .Settings.x}}{{. | printf "%s%s" .}}{{end}}`, "uses printf"},
		{"printf in an if", `{{if (printf "x")}}x{{end}}`, "uses printf"},
		{"other functions", `{{index .Settings "x"}}`, "uses index"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckTemplate("config.yaml", []byte(tt.text))
			if tt.wantError == "" {
				if err != nil {
					t.Errorf("CheckTemplate = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("CheckTemplate = %v, want %q", err, tt.wantError)
			}
		})
	}
}
