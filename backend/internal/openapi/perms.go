package openapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Permissions reads the route registrations in a directory of Go source and
// returns the permission each route requires, keyed "METHOD /path".
//
// The permission is not recoverable from the built router: chi.Walk exposes only
// the innermost handler, and the requirement is installed as middleware around
// it. Parsing the registration is therefore the only way to report the real
// requirement rather than a copy that can drift. The parse reads the same
// statement that installs the middleware, so the document and the enforcement
// cannot disagree.
//
// The permission is returned as its string value ("contacts.read"), resolved from
// the constant declared in rbacDir. Reporting the Go identifier
// ("PermContactsRead") would be useless to a client, and hard-coding the mapping
// here would be a second copy that drifts the first time a constant is renamed.
func Permissions(dir, rbacDir string) (map[string]string, error) {
	consts, err := permissionConstants(rbacDir)
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files in %s", dir)
	}

	fset := token.NewFileSet()
	out := map[string]string{}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		for _, decl := range af.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			// Route registration lives in the mount* functions by convention.
			// Anything else in this package that happens to call r.Get is not
			// part of the API surface.
			if !ok || fn.Name == nil || !strings.HasPrefix(fn.Name.Name, "mount") {
				continue
			}
			walkBody(fn.Body, "", consts, out)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no routes found in %s: the mount* convention may have changed", dir)
	}
	return out, nil
}

// permissionConstants maps a constant identifier to its string value by reading
// the rbac package's declarations.
func permissionConstants(dir string) (map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		for _, decl := range af.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if val, err := strconv.Unquote(lit.Value); err == nil {
					out[vs.Names[0].Name] = val
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no string constants found in %s", dir)
	}
	return out, nil
}

// walkBody walks the statements of a route-registration closure, carrying the
// path prefix. A Route call recurses with the extended prefix; a Group call
// recurses with the same prefix; a verb call records a route.
func walkBody(body *ast.BlockStmt, prefix string, consts, out map[string]string) {
	if body == nil {
		return
	}
	for _, stmt := range body.List {
		exprStmt, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := exprStmt.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}

		pathArg := firstStringLiteral(call)
		closure := lastFuncLit(call)

		switch sel.Sel.Name {
		case "Route":
			walkBody(closure, joinPath(prefix, pathArg), consts, out)
		case "Group":
			walkBody(closure, prefix, consts, out)
		case "Get", "Post", "Put", "Patch", "Delete":
			key := strings.ToUpper(sel.Sel.Name) + " " + joinPath(prefix, pathArg)
			out[key] = resolvePermission(permissionIn(call), consts)
		}
	}
}

// firstStringLiteral returns the first string argument, which is the path.
func firstStringLiteral(call *ast.CallExpr) string {
	for _, arg := range call.Args {
		if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				return s
			}
		}
	}
	return ""
}

// lastFuncLit returns the trailing closure argument, which is the sub-router body.
func lastFuncLit(call *ast.CallExpr) *ast.BlockStmt {
	for i := len(call.Args) - 1; i >= 0; i-- {
		if fl, ok := call.Args[i].(*ast.FuncLit); ok {
			return fl.Body
		}
	}
	return nil
}

// resolvePermission maps a constant identifier to its string value. An unknown
// identifier is returned unchanged so it is visible in the document rather than
// silently blank.
func resolvePermission(ident string, consts map[string]string) string {
	if ident == "" {
		return ""
	}
	if val, ok := consts[ident]; ok {
		return val
	}
	return ident
}

// permissionIn finds the rbac.Perm* constant in a route's receiver chain, which
// is where the permission middleware is applied.
func permissionIn(call *ast.CallExpr) string {
	perm := ""
	ast.Inspect(call, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "rbac" && strings.HasPrefix(sel.Sel.Name, "Perm") {
			perm = sel.Sel.Name
		}
		return true
	})
	return perm
}

// joinPath appends a route segment, collapsing the empty segment chi uses for a
// group root.
func joinPath(prefix, segment string) string {
	switch {
	case segment == "", segment == "/":
		if prefix == "" {
			return "/"
		}
		return prefix
	case strings.HasPrefix(segment, "/"):
		return prefix + segment
	default:
		return prefix + "/" + segment
	}
}

// PrefixAPI adds the mount prefix that the router applies to everything inside
// its /api/v1 group, so parsed keys line up with walked paths.
//
// The walk reports full paths and the parse reports paths relative to the mount
// function, so this reconciles the two. It is a named function rather than an
// inline concatenation because getting it wrong would silently produce a
// document in which every permission is missing.
func PrefixAPI(keys map[string]string) map[string]string {
	out := make(map[string]string, len(keys))
	for key, perm := range keys {
		method, path, ok := strings.Cut(key, " ")
		if !ok {
			continue
		}
		switch path {
		case "/health", "/ready":
			out[key] = perm
		default:
			out[method+" /api/v1"+path] = perm
		}
	}
	return out
}

// Report lists the routes in a stable order, for error messages and diffs.
func Report(perms map[string]string) []string {
	keys := make([]string, 0, len(perms))
	for k := range perms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
