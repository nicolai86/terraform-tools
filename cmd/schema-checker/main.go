package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var (
	debug *bool
)

type schemaCheck func(string) schemaWalker

type schemaWalker func(ast.Node) ast.Visitor

func (fn schemaWalker) Visit(node ast.Node) ast.Visitor {
	return fn(node)
}

func docChecker(fset *token.FileSet, file string) schemaWalker {
	return func(node ast.Node) ast.Visitor {
		if node == nil {
			return nil
		}

		k, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return docChecker(fset, file)
		}
		_, ok = k.Key.(*ast.BasicLit)
		if !ok {
			return nil
		}
		vs, ok := k.Value.(*ast.CompositeLit)
		if !ok {
			return docChecker(fset, file)
		}
		hasDescription := false
		for _, elt := range vs.Elts {
			name := elt.(*ast.KeyValueExpr).Key.(*ast.Ident).Name
			hasDescription = hasDescription || name == "Description"
		}
		if !hasDescription {
			fmt.Printf("%s:%#v %s", file, fset.Position(node.Pos()).Line, "Missing Description attribute")
		}
		return docChecker(fset, file)
	}
}

func schemaChecker(fset *token.FileSet, file string) schemaWalker {
	return func(node ast.Node) ast.Visitor {
		if node == nil {
			return nil
		}
		c, ok := node.(*ast.CompositeLit)
		if !ok {
			return schemaChecker(fset, file)
		}
		if c.Type == nil {
			return schemaChecker(fset, file)
		}
		return docChecker(fset, file)
	}
}

func schemaResourceChecker(fset *token.FileSet, file string) schemaWalker {
	return func(node ast.Node) ast.Visitor {
		if node == nil {
			return nil
		}
		kv, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return schemaResourceChecker(fset, file)
		}

		if v, ok := kv.Key.(*ast.Ident); !ok || v.Name != "Schema" {
			return schemaResourceChecker(fset, file)
		}
		return schemaChecker(fset, file)
	}
}

func schemaFinder(fset *token.FileSet, file string) schemaWalker {
	return func(node ast.Node) ast.Visitor {
		if node == nil {
			return nil
		}

		fn, ok := node.(*ast.FuncDecl)
		if !ok {
			return schemaFinder(fset, file)
		}
		if fn.Type.Results == nil {
			return nil
		}
		if len(fn.Type.Results.List) != 1 {
			return nil
		}
		ret, ok := fn.Type.Results.List[0].Type.(*ast.StarExpr)
		if !ok {
			return nil
		}
		sel, ok := ret.X.(*ast.SelectorExpr)
		if !ok {
			return nil
		}
		if sel.Sel.Name != "Resource" || sel.X.(*ast.Ident).Name != "schema" {
			return nil
		}
		return schemaResourceChecker(fset, file)
	}
}

func checkSchema(path string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err.Error())
	}
	ast.Walk(schemaFinder(fset, path), f)
}

func main() {
	var providerPath = flag.String("provider-path", "", "path to the terraform provider to check")
	debug = flag.Bool("debug", false, "enable debug output")
	flag.Parse()

	if providerPath == nil || *providerPath == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	log.Printf("checking schema for %q", *providerPath)
	filepath.Walk(*providerPath, func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		checkSchema(path)
		return nil
	})
}
