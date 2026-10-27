package paths

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Every location must sit under Root, so that tests (which use a temporary
// Root) can never touch the real files of the computer they run on. The
// test calls every method of Paths that returns a string, giving "x" to
// any that takes an argument (a plugin's id, for example).
func TestEveryPathIsUnderRoot(t *testing.T) {
	root := t.TempDir()
	p := Paths{Root: root}
	value := reflect.ValueOf(p)
	checked := 0
	for i := range value.NumMethod() {
		method := value.Type().Method(i)
		fn := value.Method(i)
		if fn.Type().NumOut() != 1 || fn.Type().Out(0).Kind() != reflect.String {
			continue
		}
		args := make([]reflect.Value, fn.Type().NumIn())
		for j := range args {
			if fn.Type().In(j).Kind() != reflect.String {
				t.Fatalf("%s takes a %s; teach this test what to give it", method.Name, fn.Type().In(j))
			}
			args[j] = reflect.ValueOf("x")
		}
		got := fn.Call(args)[0].String()
		if !filepath.IsAbs(got) || !strings.HasPrefix(got, root+string(filepath.Separator)) {
			t.Errorf("%s = %q, not under %q", method.Name, got, root)
		}
		checked++
	}
	if checked < 50 {
		t.Errorf("only %d paths were checked; the test isn't finding the methods", checked)
	}
}

// Two different locations must never be the same file, or one feature
// would overwrite another's data.
func TestNoTwoPathsAreTheSame(t *testing.T) {
	p := Default()
	value := reflect.ValueOf(p)
	seen := map[string]string{}
	for i := range value.NumMethod() {
		fn := value.Method(i)
		if fn.Type().NumIn() != 0 || fn.Type().NumOut() != 1 || fn.Type().Out(0).Kind() != reflect.String {
			continue
		}
		name := value.Type().Method(i).Name
		got := fn.Call(nil)[0].String()
		if other, ok := seen[got]; ok {
			t.Errorf("%s and %s are both %q", other, name, got)
		}
		seen[got] = name
	}
}

func TestDefaultIsTheRealDisk(t *testing.T) {
	if got := Default().DataDir(); !strings.HasPrefix(got, "/persistent") {
		t.Errorf("DataDir() = %q, want it on /persistent", got)
	}
}
