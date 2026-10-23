package plugin

// A plugin's templates come from its author, and rbd fills them in with Go's
// text/template before the plugin starts. Most of what text/template can
// do finishes at once, but two things can run for as long as the author
// likes: a loop ({{range 1000000000000}}), and templates that call each
// other ({{define}}, {{template}}, {{block}}), where each one calling the
// next twice doubles the work at every level. While a template is being
// filled in, the plugin stays busy, and so do backups and the other
// plugins. Templates only need to put settings into a configuration file,
// so CheckTemplate refuses these actions instead of trying to limit them.

import (
	"fmt"
	"text/template"
	"text/template/parse"
)

// TemplateFuncNames are the functions a template may call. rbd's
// fillTemplate defines them; checking a template only needs their names.
var TemplateFuncNames = []string{"bcrypt", "json", "port"}

// CheckTemplate refuses a template that doesn't parse, or that uses range,
// define, template or block.
func CheckTemplate(name string, text []byte) error {
	funcs := template.FuncMap{}
	for _, f := range TemplateFuncNames {
		funcs[f] = func(...any) string { return "" } // only the name matters here
	}
	t, err := template.New(name).Funcs(funcs).Parse(string(text))
	if err != nil {
		return fmt.Errorf("its template %s is broken: %w", name, err)
	}
	// {{define}} and {{block}} add a second template next to the file's own.
	if len(t.Templates()) > 1 {
		return fmt.Errorf("its template %s uses define or block, which plugins can't use", name)
	}
	if t.Tree == nil {
		return nil // an empty file
	}
	return checkTemplateNode(name, t.Tree.Root)
}

// checkTemplateNode looks through one part of a parsed template, and the
// parts inside it, for range and {{template}}.
func checkTemplateNode(name string, node parse.Node) error {
	switch n := node.(type) {
	case *parse.RangeNode:
		return fmt.Errorf("its template %s uses range, which plugins can't use", name)
	case *parse.TemplateNode:
		return fmt.Errorf("its template %s uses template, which plugins can't use", name)
	case *parse.ListNode:
		if n == nil {
			return nil
		}
		for _, child := range n.Nodes {
			if err := checkTemplateNode(name, child); err != nil {
				return err
			}
		}
	case *parse.IfNode:
		return checkBranches(name, n.List, n.ElseList)
	case *parse.WithNode:
		return checkBranches(name, n.List, n.ElseList)
	}
	return nil
}

// checkBranches checks both branches of an {{if}} or {{with}}.
func checkBranches(name string, branches ...*parse.ListNode) error {
	for _, branch := range branches {
		if err := checkTemplateNode(name, branch); err != nil {
			return err
		}
	}
	return nil
}
