package plugin

// A plugin's templates come from its author, and rbd fills them in with Go's
// text/template before the plugin starts. rbd runs as root and fills them in
// at every start, so a template must not be able to keep it busy for long
// or make it run out of memory. Templates only need to put settings into a
// configuration file, so CheckTemplate allows only what that needs, and
// refuses the rest instead of trying to limit it:
//   - loops ({{range 1000000000000}}) and templates that call each other
//     ({{define}}, {{template}}, {{block}}), which can run for hours;
//   - variables ({{$a := …}}, {{$a = json $a}}): reusing a value lets each
//     line double it, so 20 lines make 4 MB and 30 make 4 GB;
//   - functions that build text of any length, such as printf
//     ({{printf "%999999999d" 1}} makes 1 GB from one line).
//
// What is left: the settings and other values ({{.Settings.key}}), {{if}}
// and {{with}}, comparisons (eq, ne, lt, le, gt, ge, and, or, not), and
// RasBeacon's own functions (TemplateFuncNames). Those can't grow a value
// much from one call to the next, and fillTemplate refuses any json or html
// result larger than a whole template may be.

import (
	"fmt"
	"slices"
	"text/template"
	"text/template/parse"
)

// TemplateFuncNames are RasBeacon's functions a template may call. rbd's
// fillTemplate defines them; checking a template only needs their names.
// html is Go's own, replaced there by one that refuses huge results.
var TemplateFuncNames = []string{"bcrypt", "json", "port", "html"}

// allowedBuiltins are Go's own template functions a template may call:
// they compare values or pick one, and never build a new one.
var allowedBuiltins = []string{"eq", "ne", "lt", "le", "gt", "ge", "and", "or", "not"}

// CheckTemplate refuses a template that doesn't parse, or that uses
// anything but values, if, with and the allowed functions.
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
// parts inside it, for anything plugins can't use.
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
	case *parse.ActionNode:
		return checkPipe(name, n.Pipe)
	case *parse.IfNode:
		if err := checkPipe(name, n.Pipe); err != nil {
			return err
		}
		return checkBranches(name, n.List, n.ElseList)
	case *parse.WithNode:
		if err := checkPipe(name, n.Pipe); err != nil {
			return err
		}
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

// checkPipe checks what one action computes: no variables, and only the
// allowed functions, also inside parentheses.
func checkPipe(name string, pipe *parse.PipeNode) error {
	if pipe == nil {
		return nil
	}
	if len(pipe.Decl) > 0 {
		return fmt.Errorf("its template %s uses variables, which plugins can't use", name)
	}
	for _, cmd := range pipe.Cmds {
		for _, arg := range cmd.Args {
			if err := checkArg(name, arg); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkArg checks one word of an action: a function's name, a value, or a
// part in parentheses.
func checkArg(name string, arg parse.Node) error {
	switch a := arg.(type) {
	case *parse.IdentifierNode:
		if !slices.Contains(TemplateFuncNames, a.Ident) && !slices.Contains(allowedBuiltins, a.Ident) {
			return fmt.Errorf("its template %s uses %s, which plugins can't use", name, a.Ident)
		}
	case *parse.VariableNode:
		// $ alone is the template's own values, the same as the dot at
		// the top. Any other name would be a variable.
		if a.Ident[0] != "$" {
			return fmt.Errorf("its template %s uses variables, which plugins can't use", name)
		}
	case *parse.PipeNode:
		return checkPipe(name, a)
	case *parse.ChainNode:
		return checkArg(name, a.Node)
	}
	return nil
}
