// lint-rawptr — forbids raw *<expr> deref and &<expr> address-of in attune's
// own source, redirecting authors to internal/pkg/ptrext helpers (ptrext.Of,
// ptrext.Indirect, ptrext.IndirectOr, ...). The goal is to push read-side
// dereference panics from runtime to a single chokepoint.
//
// Two rules:
//
//	rule-deref  *p as an rvalue expression  →  ptrext.Indirect(p)/IndirectOr
//	rule-addr   &x as an address-of expr    →  ptrext.Of(x)
//
// Allowed (NOT flagged):
//   - *T in type position (params, return, struct field, type assertion, …)
//   - *p = v on the LHS of an assignment — Go has no expression form for
//     "addressable indirect", so we cannot wrap this
//   - &xs[i] addressing a slice element — wrapping copies the element
//   - &x passed to a known out-parameter API (json.Unmarshal, *Row.Scan,
//     flag.*Var, errors.As, encoding/binary.Read, …) — wrapping breaks
//     the API contract (the callee would write into a fresh copy)
//   - the ptrext package itself (it implements the helpers)
//   - generated files (// Code generated …)
//   - files containing the directive  // ptrext:file-allow  (whole file
//     skipped — use for config-binding tables and test mock-capture
//     fixtures where the out-param pattern is endemic)
//   - lines marked  // ptrext:allow  (single-site override)
//
// Exit 1 on any finding so pre-commit / CI fails red.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"unicode"
)

const (
	ruleDeref = "rule-deref"
	ruleAddr  = "rule-addr"
)

// outParamMethods — function/method names whose &arg is contractually
// "fill this in". Matching is by selector trailing identifier, which is
// loose on purpose (catches *sql.Row.Scan, *sql.Rows.Scan, fmt.Sscan, the
// json.Decoder.Decode chain, etc. without per-package lists).
var outParamMethods = map[string]struct{}{
	// decode / unmarshal / scan
	"Unmarshal":   {},
	"Decode":      {},
	"Scan":        {},
	"Claims":      {}, // go-oidc idToken.Claims(&dst)
	"Scanln":      {},
	"Sscan":       {},
	"Sscanf":      {},
	"Sscanln":     {},
	"Fscan":       {},
	"Fscanf":      {},
	"Fscanln":     {},
	"Read":        {}, // encoding/binary.Read
	"As":          {}, // errors.As(err, &target)
	"ConvertFrom": {}, // database/sql converters

	// fmt.Fprint* — the &strings.Builder / &bytes.Buffer is the write target;
	// wrapping it with ptrext.Of would write into a copy. staticcheck QF1012
	// also prefers Fprintf over WriteString(Sprintf(…)).
	"Fprint":   {},
	"Fprintf":  {},
	"Fprintln": {},

	// flag.*Var
	"Var":         {},
	"BoolVar":     {},
	"DurationVar": {},
	"Float64Var":  {},
	"IntVar":      {},
	"Int64Var":    {},
	"StringVar":   {},
	"TextVar":     {},
	"Uint64Var":   {},
	"UintVar":     {},

	// pflag (kubernetes flavor) — same shape as flag.*Var
	"StringVarP":   {},
	"BoolVarP":     {},
	"IntVarP":      {},
	"DurationVarP": {},

	// attune internal decoders that fan into json.Unmarshal — same contract
	// as the stdlib ones, just wrapped behind a helper.
	"postJSON":   {}, // internal/infra/lark/client.go
	"decodeJSON": {}, // internal/handlers/console/inbound/inbound_handler.go
}

var (
	excludeRe    = regexp.MustCompile(`(?:\.pb\.go$|/proto/gen/|/migrations/|/testdata/)`)
	generatedRe  = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.`)
	allowLinePat = regexp.MustCompile(`//\s*ptrext:allow\b`)
	fileAllowPat = regexp.MustCompile(`//\s*ptrext:file-allow\b`)
)

type finding struct {
	Pos  token.Position
	Rule string
	Msg  string
}

func main() {
	verbose := flag.Bool("v", false, "verbose: also print allowed sites")
	flag.Parse()
	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"./internal/...", "./cmd/..."}
	}

	files, err := expandPaths(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lint-rawptr:", err)
		os.Exit(2)
	}

	fset := token.NewFileSet()
	var all []finding
	for _, f := range files {
		fnd, err := analyzeFile(fset, f, *verbose)
		if err != nil {
			fmt.Fprintln(os.Stderr, f, ":", err)
			continue
		}
		all = append(all, fnd...)
	}

	for _, f := range all {
		fmt.Printf("%s:%d:%d: %s — %s\n", f.Pos.Filename, f.Pos.Line, f.Pos.Column, f.Rule, f.Msg)
	}

	if n := len(all); n > 0 {
		fmt.Fprintf(os.Stderr, "\nlint-rawptr: %d finding(s)\n", n)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "lint-rawptr: clean")
}

func expandPaths(args []string) ([]string, error) {
	var out []string
	for _, p := range args {
		p = strings.TrimPrefix(p, "./")
		p = strings.TrimSuffix(p, "/...")
		p = strings.TrimSuffix(p, "/")
		err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "vendor" || d.Name() == "node_modules" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if excludeRe.MatchString(path) {
				return nil
			}
			// the helpers themselves
			if strings.Contains(path, "internal/pkg/ptrext/") {
				return nil
			}
			// the linter itself — it walks the AST, so it's all type-position
			// hits; just skip to avoid noise.
			if strings.Contains(path, "cmd/lint-rawptr/") || strings.Contains(path, "cmd/lint-errorcode/") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func analyzeFile(fset *token.FileSet, path string, verbose bool) ([]finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if generatedRe.Match(src) {
		return nil, nil
	}
	if fileAllowPat.Match(src) {
		return nil, nil
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	allow := allowLineSet(file, fset)
	typeStars := classifyTypeStars(file)

	a := &analyzer{
		fset:      fset,
		file:      file,
		typeStars: typeStars,
		allow:     allow,
	}
	a.walk(file, nil)
	return a.findings, nil
}

// allowLineSet — every source line carrying a // ptrext:allow marker.
func allowLineSet(file *ast.File, fset *token.FileSet) map[int]bool {
	out := map[int]bool{}
	for _, g := range file.Comments {
		for _, c := range g.List {
			if allowLinePat.MatchString(c.Text) {
				out[fset.Position(c.Slash).Line] = true
			}
		}
	}
	return out
}

// classifyTypeStars — pre-pass: every StarExpr appearing in a TYPE position
// (vs a deref expression position). We walk explicitly so each child gets
// the right context — ast.Walk's single-Visitor model can't differentiate
// per-child. The walk is split into per-shape helpers to keep individual
// function complexity bounded; each helper returns true when it consumes
// the node so the next one can be tried.
func classifyTypeStars(root ast.Node) map[*ast.StarExpr]struct{} {
	c := &typeClassifier{set: map[*ast.StarExpr]struct{}{}}
	c.walk(root, false)
	return c.set
}

type typeClassifier struct {
	set map[*ast.StarExpr]struct{}
}

// isNilNode catches both untyped nil and typed-nil (e.g. a (*ast.FieldList)(nil)
// carried in an ast.Node interface, which is what ast fields like FuncType.Recv
// look like when unset).
func isNilNode(n ast.Node) bool {
	if n == nil {
		return true
	}
	v := reflect.ValueOf(n)
	return v.Kind() == reflect.Ptr && v.IsNil()
}

func (c *typeClassifier) walk(n ast.Node, inType bool) {
	if isNilNode(n) {
		return
	}
	if star, ok := n.(*ast.StarExpr); ok {
		if inType {
			c.set[star] = struct{}{}
		}
		c.walk(star.X, inType)
		return
	}
	if c.walkFuncish(n, inType) {
		return
	}
	if c.walkTypeContainer(n, inType) {
		return
	}
	if c.walkSpecOrLit(n, inType) {
		return
	}
	if c.walkExprChild(n, inType) {
		return
	}
	c.walkDefault(n)
}

// walkFuncish handles function-shaped nodes whose receivers/params/results
// are types but whose body is not.
func (c *typeClassifier) walkFuncish(n ast.Node, inType bool) bool {
	switch x := n.(type) {
	case *ast.FuncDecl:
		c.walk(x.Recv, true)
		c.walk(x.Type, inType)
		c.walk(x.Body, false)
	case *ast.FuncType:
		c.walk(x.TypeParams, true)
		c.walk(x.Params, true)
		c.walk(x.Results, true)
	case *ast.FuncLit:
		c.walk(x.Type, false) // FuncType handles its own children
		c.walk(x.Body, false)
	default:
		return false
	}
	return true
}

// walkTypeContainer handles nodes whose direct children are all in type
// position (or trivially so — TypeAssertExpr.X is the exception).
func (c *typeClassifier) walkTypeContainer(n ast.Node, _ bool) bool {
	switch x := n.(type) {
	case *ast.FieldList:
		for _, f := range x.List {
			c.walk(f, true)
		}
	case *ast.Field:
		c.walk(x.Type, true)
	case *ast.MapType:
		c.walk(x.Key, true)
		c.walk(x.Value, true)
	case *ast.ChanType:
		c.walk(x.Value, true)
	case *ast.ArrayType:
		c.walk(x.Len, false)
		c.walk(x.Elt, true)
	case *ast.StructType:
		c.walk(x.Fields, true)
	case *ast.InterfaceType:
		c.walk(x.Methods, true)
	case *ast.TypeAssertExpr:
		c.walk(x.X, false)
		c.walk(x.Type, true)
	default:
		return false
	}
	return true
}

// walkSpecOrLit handles declarations and composite-shape nodes that mix
// type and value positions.
func (c *typeClassifier) walkSpecOrLit(n ast.Node, _ bool) bool {
	switch x := n.(type) {
	case *ast.ValueSpec:
		c.walk(x.Type, true)
		for _, v := range x.Values {
			c.walk(v, false)
		}
	case *ast.TypeSpec:
		c.walk(x.TypeParams, true)
		c.walk(x.Type, true)
	case *ast.CompositeLit:
		c.walk(x.Type, true)
		for _, elt := range x.Elts {
			c.walk(elt, false)
		}
	case *ast.CallExpr:
		c.walkCallFun(x.Fun)
		for _, a := range x.Args {
			c.walk(a, false)
		}
	default:
		return false
	}
	return true
}

// walkExprChild handles plain expression-context nodes that need targeted
// recursion (paren/selector/index forms).
func (c *typeClassifier) walkExprChild(n ast.Node, inType bool) bool {
	switch x := n.(type) {
	case *ast.ParenExpr:
		c.walk(x.X, inType)
	case *ast.SelectorExpr:
		c.walk(x.X, false)
	case *ast.IndexExpr:
		indexIsType := inType || isLikelySelectorInstantiation(x.X)
		c.walk(x.X, inType)
		c.walk(x.Index, indexIsType)
	case *ast.IndexListExpr:
		indexIsType := inType || isLikelySelectorInstantiation(x.X)
		c.walk(x.X, inType)
		for _, ix := range x.Indices {
			c.walk(ix, indexIsType)
		}
	default:
		return false
	}
	return true
}

func (c *typeClassifier) walkCallFun(fun ast.Expr) {
	switch x := fun.(type) {
	case *ast.ParenExpr:
		c.walkCallFun(x.X)
	case *ast.IndexExpr:
		c.walk(x.X, false)
		c.walk(x.Index, true)
	case *ast.IndexListExpr:
		c.walk(x.X, false)
		for _, ix := range x.Indices {
			c.walk(ix, true)
		}
	default:
		c.walk(fun, isTypeExpr(fun))
	}
}

func isLikelySelectorInstantiation(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return isLikelySelectorInstantiation(x.X)
	case *ast.SelectorExpr:
		return startsUpper(x.Sel.Name)
	}
	return false
}

func startsUpper(s string) bool {
	for _, r := range s {
		return unicode.IsUpper(r)
	}
	return false
}

// walkDefault — fallthrough recursion via ast.Inspect for the structural
// nodes none of the above cared about (BlockStmt, ExprStmt, AssignStmt,
// BinaryExpr, UnaryExpr, IfStmt, ForStmt, RangeStmt, ReturnStmt,
// KeyValueExpr, ...). They're all non-type-defining, so their children
// inherit inType=false.
func (c *typeClassifier) walkDefault(n ast.Node) {
	ast.Inspect(n, func(child ast.Node) bool {
		if child == nil || child == n {
			return child != nil
		}
		c.walk(child, false)
		return false
	})
}

func isTypeExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return isTypeExpr(x.X)
	case *ast.StarExpr,
		*ast.ArrayType,
		*ast.MapType,
		*ast.ChanType,
		*ast.FuncType,
		*ast.InterfaceType,
		*ast.StructType:
		return true
	}
	return false
}

type analyzer struct {
	fset      *token.FileSet
	file      *ast.File
	typeStars map[*ast.StarExpr]struct{}
	allow     map[int]bool
	findings  []finding
	// parent stack — we push before recursing, pop after, so we can ask
	// "what is the parent of the current node" for the LHS-of-assign and
	// out-param-arg checks.
	stack []ast.Node
}

func (a *analyzer) walk(n ast.Node, parent ast.Node) {
	if n == nil {
		return
	}
	if parent != nil {
		a.stack = append(a.stack, parent)
		defer func() { a.stack = a.stack[:len(a.stack)-1] }()
	}

	switch x := n.(type) {
	case *ast.StarExpr:
		// type-position StarExpr is fine; deref is the bad rvalue case
		if _, isType := a.typeStars[x]; !isType {
			// allow LHS of assignment: *p = v
			if !a.isAssignLHS(x) {
				if !a.allow[a.fset.Position(x.Star).Line] {
					a.report(x.Star, ruleDeref,
						"raw pointer deref — use ptrext.Indirect(p) / ptrext.IndirectOr(p, fallback)")
				}
			}
		}

	case *ast.UnaryExpr:
		if x.Op == token.AND {
			// allowed: &xs[i]
			if _, ok := x.X.(*ast.IndexExpr); !ok {
				// allowed: out-param call
				if !a.isOutParamArg(x) {
					if !a.allow[a.fset.Position(x.OpPos).Line] {
						a.report(x.OpPos, ruleAddr,
							"raw address-of — use ptrext.Of(value)")
					}
				}
			}
		}
	}

	// recurse
	ast.Inspect(n, func(c ast.Node) bool {
		if c == nil || c == n {
			return c != nil
		}
		a.walk(c, n)
		return false
	})
}

// isAssignLHS — true if star is on the LHS of an assignment (*p = v).
// Looks at the immediate parent for AssignStmt with Lhs containing this node.
func (a *analyzer) isAssignLHS(star *ast.StarExpr) bool {
	if len(a.stack) == 0 {
		return false
	}
	parent := a.stack[len(a.stack)-1]
	as, ok := parent.(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, l := range as.Lhs {
		if l == star {
			return true
		}
	}
	return false
}

// isOutParamArg — true if the &expr is a direct arg of an allowlisted call.
// e.g. json.Unmarshal(data, &out) — the &out's parent is the CallExpr.
func (a *analyzer) isOutParamArg(amp *ast.UnaryExpr) bool {
	if len(a.stack) == 0 {
		return false
	}
	parent := a.stack[len(a.stack)-1]
	call, ok := parent.(*ast.CallExpr)
	if !ok {
		return false
	}
	// Confirm amp is in call.Args (not call.Fun).
	inArgs := false
	for _, arg := range call.Args {
		if arg == amp {
			inArgs = true
			break
		}
	}
	if !inArgs {
		return false
	}
	name := calleeName(call.Fun)
	_, ok = outParamMethods[name]
	return ok
}

// calleeName — last identifier in the call's Fun expression, e.g.
// json.Unmarshal → "Unmarshal", row.Scan → "Scan", Decode → "Decode".
func calleeName(fun ast.Expr) string {
	switch x := fun.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.IndexExpr:
		return calleeName(x.X)
	case *ast.IndexListExpr:
		return calleeName(x.X)
	case *ast.ParenExpr:
		return calleeName(x.X)
	}
	return ""
}

func (a *analyzer) report(pos token.Pos, rule, msg string) {
	a.findings = append(a.findings, finding{
		Pos:  a.fset.Position(pos),
		Rule: rule,
		Msg:  msg,
	})
}
