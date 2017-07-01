package main

import (
	"bufio"
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
	"path/filepath"
	"reflect"
	"strings"
)

type resourceDefinition struct {
	name string
	fnc  string
}

type provider struct {
	datasources []resourceDefinition
	resources   []resourceDefinition
}

// documentation contains all documentation for datasources, resources, indexed by name.
// provider prefixes have been removed, if a datasource or resource is missing either
// the documentation is missing or the file didn't allow classification.
type documentation struct {
	Datasources map[string][]byte
	Resources   map[string][]byte
}

type docType int

var (
	docTypeDatasource docType = 0
	docTypeResource   docType = 1
	debug             *bool
)

func Debugf(format string, a ...interface{}) {
	if *debug {
		log.Printf(format, a...)
	}
}

func loadDocumentation(providerName, root string, extensions []string) (documentation, error) {
	d := documentation{
		Datasources: map[string][]byte{},
		Resources:   map[string][]byte{},
	}
	candidate := func(path string) bool {
		for _, ext := range extensions {
			if strings.HasSuffix(path, ext) {
				return true
			}
		}
		return false
	}
	return d, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if !candidate(path) {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		docset, err := ioutil.ReadAll(f)
		if err != nil {
			return err
		}

		docName, docType, err := classifyDoc(providerName, path, docset)
		if err != nil {
			Debugf("Ignoring %q due to %v", path, err)
			return nil
		}

		if docType == docTypeDatasource {
			d.Datasources[docName] = docset
		} else {
			d.Resources[docName] = docset
		}

		return nil
	})
}

func classifyDoc(providerName, path string, content []byte) (string, docType, error) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var t docType
	var n string
	var err error

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "sidebar_current") {
			continue
		}
		parts := strings.Split(line, ": ")
		line = parts[1][1 : len(parts[1])-1]
		Debugf("%#v\n", parts)
		if strings.HasPrefix(line, "docs-") {
			line = line[5:]
		}
		if strings.HasPrefix(line, providerName+"-") {
			line = line[len(providerName)+1:]
		}
		if strings.Contains(line, "datasource") || strings.Contains(line, "data-source") || strings.Contains(path, "/d/") || strings.Contains(path, "data_source") {
			t = docTypeDatasource
			if strings.HasPrefix(line, "datasource-") {
				line = line[11:]
			}
			if strings.Contains(line, "datasource-") {
				idx := strings.LastIndex(line, "datasource-")
				line = line[idx+11:]
			}

			n = line[:]
			break
		}
		if strings.Contains(line, "resource") || strings.Contains(path, "/r/") {
			t = docTypeResource
			if strings.HasPrefix(line, "resource-") {
				line = line[9:]
			}
			if strings.Contains(line, "resource-") {
				idx := strings.LastIndex(line, "resource-")
				line = line[idx+9:]
			}
			n = line[:]
			break
		}
	}
	if n == "" {
		err = fmt.Errorf("could not find sidebar_current")
	} else {
		n = providerName + "_" + strings.Replace(strings.Replace(n, " ", "_", -1), "-", "_", -1)
	}
	return n, t, err
}

func main() {
	var providerName = flag.String("provider-name", "", "prefix name of the provider")
	var providerPath = flag.String("provider-path", "", "path to the terraform provider to check")
	debug = flag.Bool("debug", false, "enable debug output")
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
	Debugf("%#v\n", prov)

	docs, err := loadDocumentation(*providerName, path.Join(*providerPath, "..", "website"), []string{"md", "markdown", "html.md", "html.markdown"})
	if err != nil {
		log.Fatalf("Failed to load docs: %v", err)
	}
	Debugf("Datasources:\n")
	for k, v := range docs.Datasources {
		Debugf("docs of %q: %d\n", k, len(v))
	}
	Debugf("Resources:\n")
	for k, v := range docs.Resources {
		Debugf("docs of %q: %d\n", k, len(v))
	}

	filepath.Walk(*providerPath, func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		verifyAttributes(path, prov, docs)
		return nil
	})
}

func verifyAttributes(path string, prov provider, docs documentation) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		log.Printf("Failed to parse %s: %s\n", path, err)
		return
	}

	// TODO identify type
	for _, decl := range f.Decls {
		fncDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fncDecl.Type == nil {
			continue
		}
		if fncDecl.Type.Results == nil {
			continue
		}
		if len(fncDecl.Type.Results.List) != 1 {
			Debugf("Ignoring %s because arity doesn't match\n", fncDecl.Name.String())
			continue
		}
		retExpr, ok := fncDecl.Type.Results.List[0].Type.(*ast.StarExpr)
		if !ok {
			Debugf("Ignoring %q because return type %q doesn't match %q\n", fncDecl.Name.String(), reflect.TypeOf(fncDecl.Type.Results.List[0].Type), "*ast.StarExpr")
			continue
		}
		selExpr, ok := retExpr.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}

		// 	// TODO verify the import path of schema is correct
		if selExpr.Sel.Name != "Resource" || selExpr.X.(*ast.Ident).Name != "schema" {
			continue
		}

		var schemaType docType
		var schemaName string
		found := false
		var docset []byte
		for _, v := range prov.datasources {
			if v.fnc == fncDecl.Name.Name {
				schemaType = docTypeDatasource
				schemaName = v.name
				found = true
				docset = docs.Datasources[v.name]
			}
		}
		for _, v := range prov.resources {
			if v.fnc == fncDecl.Name.Name {
				schemaType = docTypeResource
				schemaName = v.name
				found = true
				docset = docs.Resources[v.name]
			}
		}
		if !found {
			Debugf("Could not find matching datasource or resource for %v\n", fncDecl.Name.Name)
			continue
		}
		_ = schemaType
		_ = schemaName

		retExp, ok := fncDecl.Body.List[0].(*ast.ReturnStmt)
		if !ok {
			Debugf("TODO structure of %v does not allow parsing yet\n", fncDecl.Name.Name)
			continue
		}
		for _, elt := range retExp.Results[0].(*ast.UnaryExpr).X.(*ast.CompositeLit).Elts {
			eltt, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if eltt.Key.(*ast.Ident).Name != "Schema" {
				continue
			}

			// TODO parse recursive maps, sets, etc
			schemaElt, ok := eltt.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, elt := range schemaElt.Elts {
				eltt, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					Debugf("ignoring…\n")
					continue
				}

				expectedMarkup := ""
				name := ""
				if basic, ok := eltt.Key.(*ast.BasicLit); ok {
					name = basic.Value
					expectedMarkup = fmt.Sprintf("`%s`", decodeString(basic.Value))
				} else {
					// TODO support constants defined elsewhere…
					lit := eltt.Key.(*ast.Ident).Obj.Decl.(*ast.ValueSpec).Values[0].(*ast.BasicLit)
					name = lit.Value
					expectedMarkup = fmt.Sprintf("`%s`", decodeString(lit.Value))
				}
				if !bytes.Contains(docset, []byte(expectedMarkup)) {
					log.Printf("Missing %q in docs of %q\n", expectedMarkup, schemaName)
				}
				_ = name

				// if elv, ok := eltt.Value.(*ast.CompositeLit); ok {
				// 	for _, elv := range elv.Elts {
				// 		elvv := elv.(*ast.KeyValueExpr)
				// 		log.Printf("%s: %#v\n", decodeString(name), elvv.Key.(*ast.Ident).Name)
				// 	}
				// }
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
						p.resources = append(p.resources, resourceDefinition{
							name: decodeString(eltttt.Key.(*ast.BasicLit).Value),
							fnc:  eltttt.Value.(*ast.CallExpr).Fun.(*ast.Ident).Name,
						})
					}
				case elttKey.Name == "DataSourcesMap":
					elttValue := elt.(*ast.KeyValueExpr).Value.(*ast.CompositeLit)
					for _, elttt := range elttValue.Elts {
						eltttt := elttt.(*ast.KeyValueExpr)
						p.datasources = append(p.datasources, resourceDefinition{
							name: decodeString(eltttt.Key.(*ast.BasicLit).Value),
							fnc:  eltttt.Value.(*ast.CallExpr).Fun.(*ast.Ident).Name,
						})
					}
				default:
					Debugf("ignoring provider keys %#v\n", elttKey.Name)
				}
			}
		}
	}
	return p, nil
}
