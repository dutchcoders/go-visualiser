package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	marija, err := New(WithURL("https://demo.marija.io/submit?honeytrap"))
	if err != nil {
		fmt.Println(err)
		return
	}

	marija.Start(ctx)

	do := func(p string) {
		fset := token.NewFileSet() // positions are relative to fset

		// Parse src but stop after processing the imports.
		f, err := parser.ParseFile(fset, p, nil, parser.AllErrors)
		if err != nil {
			fmt.Println(err)
			return
		}

		// Print the imports from the file's AST.
		imports := []string{}

		for _, s := range f.Imports {
			imports = append(imports, s.Path.Value)
		}

		m := map[string]interface{}{}
		m["imports"] = imports
		m["file"] = "main.go"
		marija.Send(m)

		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			name := fd.Name.Name

			references := []string{}

			var inspectFunc func(ast.Node) bool
			inspectFunc = func(n ast.Node) bool {
				fmt.Printf("%#+v\n", n)
				// For selector expressions, only inspect the left hand side.
				// (For an expression like fmt.Println, only add "fmt" to the
				// set of unresolved names, not "Println".)
				if e, ok := n.(*ast.SelectorExpr); ok {
					ast.Inspect(e.X, inspectFunc)
					return false
				}
				// For key value expressions, only inspect the value
				// as the key should be resolved by the type of the
				// composite literal.
				if e, ok := n.(*ast.KeyValueExpr); ok {
					ast.Inspect(e.Value, inspectFunc)
					return false
				}

				if ce, ok := n.(*ast.CallExpr); ok {
					fmt.Printf("%#+v\n", ce, ce.Fun)

					if se, ok := ce.Fun.(*ast.SelectorExpr); ok {
						references = append(references, fmt.Sprintf("%s.%s", se.X, se.Sel))
					}
				}

				if id, ok := n.(*ast.Ident); ok {
					fmt.Printf("%#+v\n", id)
					if id.Obj != nil {
						fmt.Println("obj", id.Name, id.Obj, id.String(), id.Obj)
					} else {
						fmt.Println(id.Name, id.Obj, id.String())
					}
				}

				return true
			}

			ast.Inspect(fd.Body, inspectFunc)

			m := map[string]interface{}{}
			m["file"] = "main.go"
			m["name"] = fmt.Sprintf("%s.%s", f.Name, name)
			marija.Send(m)

			m = map[string]interface{}{}
			m["references"] = references
			m["name"] = fmt.Sprintf("%s.%s", f.Name, name)
			marija.Send(m)
		}
	}

	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		do(path)
		return nil
	})

	if err != nil {
		fmt.Println(err.Error())
	}

	time.Sleep(10 * time.Minute)
}
