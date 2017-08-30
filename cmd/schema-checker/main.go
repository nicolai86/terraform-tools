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
	debug        *bool
	providerPath *string
)

type schemaCheck func(string) schemaWalker

type schemaWalker func(ast.Node) ast.Visitor

func (fn schemaWalker) Visit(node ast.Node) ast.Visitor {
	return fn(node)
}

func debugWalker() schemaWalker {
	return schemaWalker(func(n ast.Node) ast.Visitor {
		fmt.Printf("%#v\n", n)
		return debugWalker()
	})
}

type checkFn func(attributeName string, def *ast.CompositeLit, schema ast.Node) error

func checkFnFunc(fn func(attributeName string, def *ast.CompositeLit, schema ast.Node) error) checkFn {
	return checkFn(fn)
}

func checkDescription(attributeName string, def *ast.CompositeLit, schema ast.Node) error {
	hasDescription := false
	for _, elt := range def.Elts {
		name := elt.(*ast.KeyValueExpr).Key.(*ast.Ident).Name
		hasDescription = hasDescription || name == "Description"
	}
	if hasDescription {
		return nil
	}
	return fmt.Errorf("%s: Missing Description attribute", attributeName)

}

func collectAttributeNames(schema ast.Node) []string {
	names := []string{}
	ast.Walk(attributeCollector(&names), schema)
	return names
}

func collectConflicts(node ast.Node) []string {
	conflicts := []string{}
	ast.Walk(debugWalker(), node)
	return conflicts
}

func checkConflictsWith(attributeName string, def *ast.CompositeLit, schema ast.Node) error {
	conflicts := []string{}

	for _, elt := range def.Elts {
		name := elt.(*ast.KeyValueExpr).Key.(*ast.Ident).Name
		if name != "ConflictsWith" {
			continue
		}
		for _, conflict := range elt.(*ast.KeyValueExpr).Value.(*ast.CompositeLit).Elts {
			value := conflict.(*ast.BasicLit).Value
			conflicts = append(conflicts, value[1:len(value)-1])
		}
	}

	if len(conflicts) == 0 {
		return nil
	}
	attributeNames := collectAttributeNames(schema)

	errors := []error{}
	for _, conflict := range conflicts {
		_ = conflict
		exists := false
		for _, attribute := range attributeNames {
			if attribute == conflict {
				exists = true
				break
			}
		}
		if !exists {
			errors = append(errors, fmt.Errorf("conflict target %q does not exist", conflict))
		}
	}
	if len(errors) == 0 {
		return nil
	}
	errorMessages := []string{}
	for _, err := range errors {
		errorMessages = append(errorMessages, err.Error())
	}
	return fmt.Errorf("%s: %s", attributeName, strings.Join(errorMessages, ", "))
}

func checkAttributeName(attributeName string, def *ast.CompositeLit, schema ast.Node) error {
	if attributeName == "id" {
		return fmt.Errorf("%s: attribute name is reserved", attributeName)
	}
	return nil
}

var checks = []checkFn{
	checkFnFunc(checkDescription),
	checkFnFunc(checkAttributeName),
	checkFnFunc(checkConflictsWith),
}

func attributeCollector(res *[]string) schemaWalker {
	return func(node ast.Node) ast.Visitor {
		if node == nil {
			return nil
		}

		k, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return attributeCollector(res)
		}
		lit, ok := k.Key.(*ast.BasicLit)
		if !ok {
			return nil
		}
		*res = append(*res, lit.Value[1:len(lit.Value)-1])
		return nil
	}
}

func attributeChecker(fset *token.FileSet, file string, schema ast.Node) schemaWalker {
	return func(node ast.Node) ast.Visitor {
		if node == nil {
			return nil
		}

		k, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return attributeChecker(fset, file, schema)
		}
		lit, ok := k.Key.(*ast.BasicLit)
		if !ok {
			return nil
		}

		vs, ok := k.Value.(*ast.CompositeLit)
		if !ok {
			return attributeChecker(fset, file, schema)
		}

		for _, check := range checks {
			err := check(lit.Value, vs, schema)
			if err != nil {
				fmt.Printf("%s:%#v %s\n", strings.Replace(file, *providerPath, "", -1), fset.Position(node.Pos()).Line, err.Error())
			}
		}

		return attributeChecker(fset, file, schema)
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
		return attributeChecker(fset, file, c)
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

func init() {
	providerPath = flag.String("provider-path", "", "path to the terraform provider to check")
	debug = flag.Bool("debug", false, "enable debug output")
	flag.Parse()

	if providerPath == nil || *providerPath == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}
}

func main() {
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
