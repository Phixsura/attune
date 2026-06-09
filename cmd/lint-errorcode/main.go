// lint-errorcode — forbids hand-written ErrorResponse.Code string literals.
//
// ErrorResponse.code intentionally remains a string protobuf field for
// backwards compatibility, but writers must pass attunev1.ErrorCode and let
// dispatcher/respond serialize code.String(). This lint catches the easy drift
// vector: attunev1.ErrorResponse{Code: "..."}.
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
	"regexp"
	"strings"
)

const ruleCodeLiteral = "rule-errorresponse-code-literal"

var (
	excludeRe    = regexp.MustCompile(`(?:\.pb\.go$|/migrations/|/testdata/)`)
	generatedRe  = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.`)
	allowLinePat = regexp.MustCompile(`//\s*errorcode:allow\b`)
	fileAllowPat = regexp.MustCompile(`//\s*errorcode:file-allow\b`)
)

type finding struct {
	Pos  token.Position
	Rule string
	Msg  string
}

func main() {
	flag.Parse()
	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"./internal/...", "./cmd/..."}
	}

	files, err := expandPaths(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lint-errorcode:", err)
		os.Exit(2)
	}

	fset := token.NewFileSet()
	var all []finding
	for _, f := range files {
		fnd, err := analyzeFile(fset, f)
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
		fmt.Fprintf(os.Stderr, "\nlint-errorcode: %d finding(s)\n", n)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "lint-errorcode: clean")
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
			if strings.Contains(path, "cmd/lint-errorcode/") {
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

func analyzeFile(fset *token.FileSet, path string) ([]finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if generatedRe.Match(src) || fileAllowPat.Match(src) {
		return nil, nil
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	allow := allowLineSet(file, fset)
	var out []finding
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isErrorResponseType(lit.Type) {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok || !isCodeKey(kv.Key) || !isStringLiteral(kv.Value) {
				continue
			}
			pos := fset.Position(kv.Value.Pos())
			if allow[pos.Line] {
				continue
			}
			out = append(out, finding{
				Pos:  pos,
				Rule: ruleCodeLiteral,
				Msg:  `use attunev1.ErrorCode_*.String() or dispatcher.Fail/NewError/Reject with attunev1.ErrorCode_* instead of a raw code string`,
			})
		}
		return true
	})
	return out, nil
}

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

func isErrorResponseType(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name == "ErrorResponse"
	case *ast.SelectorExpr:
		return x.Sel.Name == "ErrorResponse"
	}
	return false
}

func isCodeKey(expr ast.Expr) bool {
	key, ok := expr.(*ast.Ident)
	return ok && key.Name == "Code"
}

func isStringLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}
