package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// These rules keep a new transport from becoming the owner of a feature or a database.
func TestDependencyBoundaries(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	const module = "github.com/prayujt/wavelength/"
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		transport := strings.HasPrefix(relative, "internal/transport/")
		application := strings.HasPrefix(relative, "internal/app/")
		database := strings.HasPrefix(relative, "internal/database/")
		source, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, spec := range source.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(dependency, module+"internal/transport/") && !transport && !application {
				t.Errorf("%s imports a transport: %s", relative, dependency)
			}
			if !application && !database && dependency == module+"internal/database" {
				t.Errorf("%s imports the database implementation", relative)
			}
			if transport && dependency == module+"internal/config" {
				t.Errorf("%s bypasses application wiring: %s", relative, dependency)
			}
		}
		if transport {
			ast.Inspect(source, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				owner, ok := selector.X.(*ast.Ident)
				if ok && owner.Name == "os" && (selector.Sel.Name == "Getenv" || selector.Sel.Name == "LookupEnv") {
					t.Errorf("%s reads process configuration", relative)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
