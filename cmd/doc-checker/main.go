package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path"
	"reflect"
)

type provider struct {
	datasources []string
	resources   []string
}

func main() {
	var providerName = flag.String("provider-name", "", "prefix name of the provider")
	var providerPath = flag.String("provider-path", "", "path to the terraform provider to check")
	flag.Parse()

	if providerPath == nil || *providerPath == "" || providerName == nil || *providerName == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	log.Printf("checking documentation for %q", *providerPath)
	prov, err := parseProviderDefinition(fmt.Sprintf("%s/provider.go", *providerPath))
	if err != nil {
		log.Fatalf("Failed to parse the provider: %q", err)
	}
	log.Printf("%#v\n", prov)

	for _, r := range prov.resources {
		resourceFile := fmt.Sprintf("%s.html.markdown", r[len(*providerName)+1:])
		resourcePath := path.Join(*providerPath, "..", "website", "docs", "r", resourceFile)
		if err := fileExists(resourcePath); err != nil {
			log.Printf("resource documentation %q is missing at %q", resourceFile, resourcePath)
			continue
		}

		verifyResourceAttributes(
			path.Join(*providerPath, fmt.Sprintf("resource_%s.go", r)),
			resourcePath,
		)
	}

	for _, ds := range prov.datasources {
		datasourceFile := fmt.Sprintf("%s.html.markdown", ds[len(*providerName)+1:])
		datasourcePath := path.Join(*providerPath, "..", "website", "docs", "d", datasourceFile)
		if err := fileExists(datasourcePath); err != nil {
			log.Printf("resource documentation %q is missing at %q", datasourceFile, datasourcePath)
			continue
		}

		verifyResourceAttributes(
			path.Join(*providerPath, fmt.Sprintf("data_source_%s.go", ds)),
			datasourcePath,
		)
	}
}

func verifyResourceAttributes(sourceFile, docFile string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, sourceFile, nil, parser.ParseComments)
	if err != nil {
		log.Printf("Failed to parse %s", sourceFile)
		return
	}

	docF, err := os.Open(docFile)
	if err != nil {
		log.Printf("Failed to open %q\n", docFile)
		return
	}
	docset, _ := ioutil.ReadAll(docF)

	for _, decl := range f.Decls {
		fncDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if len(fncDecl.Type.Results.List) != 1 {
			log.Printf("Ignoring %s because arity doesn't match\n", fncDecl.Name.String())
			continue
		}
		retExpr, ok := fncDecl.Type.Results.List[0].Type.(*ast.StarExpr)
		if !ok {
			log.Printf("Ignoring %q because return type %q doesn't match %q\n", fncDecl.Name.String(), reflect.TypeOf(fncDecl.Type.Results.List[0].Type), "*ast.StarExpr")
			continue
		}
		selExpr, ok := retExpr.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}

		// TODO verify the import path of schema is correct
		if selExpr.Sel.Name != "Resource" || selExpr.X.(*ast.Ident).Name != "schema" {
			continue
		}

		for _, elt := range fncDecl.Body.List[0].(*ast.ReturnStmt).Results[0].(*ast.UnaryExpr).X.(*ast.CompositeLit).Elts {
			eltt, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if eltt.Key.(*ast.Ident).Name != "Schema" {
				continue
			}

			// TODO parse recursive maps, sets, etc
			schemaElt := eltt.Value.(*ast.CompositeLit)
			for _, elt := range schemaElt.Elts {
				eltt, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}

				expectedMarkup := fmt.Sprintf("* `%s`", decodeString(eltt.Key.(*ast.BasicLit).Value))
				if !bytes.Contains(docset, []byte(expectedMarkup)) {
					log.Printf("Missing %q in %q\n", expectedMarkup, docFile)
				}
			}
		}
	}
}

func decodeString(val string) string {
	return val[1 : len(val)-1]
}

func fileExists(filePath string) error {
	_, err := os.Stat(filePath)
	return err
}

// parseProviderDefinition takes a provider.go file and tries to extract the declared
// datasources and resources from the AST
func parseProviderDefinition(path string) (provider, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return provider{}, err
	}
	p := provider{}
	for _, decl := range f.Decls {
		v, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if v.Name.String() != "Provider" {
			continue
		}

		for _, stmt := range v.Body.List {
			ret, ok := stmt.(*ast.ReturnStmt)
			if !ok {
				continue
			}
			st := ret.Results[0].(*ast.UnaryExpr).X.(*ast.CompositeLit)

			for _, elt := range st.Elts {
				elttKey := elt.(*ast.KeyValueExpr).Key.(*ast.Ident)
				switch {
				case elttKey.Name == "ResourcesMap":
					elttValue := elt.(*ast.KeyValueExpr).Value.(*ast.CompositeLit)
					for _, elttt := range elttValue.Elts {
						eltttt := elttt.(*ast.KeyValueExpr)
						p.resources = append(p.resources, decodeString(eltttt.Key.(*ast.BasicLit).Value))
					}
				case elttKey.Name == "DataSourcesMap":
					elttValue := elt.(*ast.KeyValueExpr).Value.(*ast.CompositeLit)
					for _, elttt := range elttValue.Elts {
						eltttt := elttt.(*ast.KeyValueExpr)
						p.datasources = append(p.datasources, decodeString(eltttt.Key.(*ast.BasicLit).Value))
					}
				default:
					log.Printf("ignoring provider keys %#v\n", elttKey.Name)
				}
			}
		}
	}
	return p, nil
}
