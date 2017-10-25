package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"html/template"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	g "github.com/terraform-providers/terraform-provider-google/google"
)

func main() {
	fmt.Println("hey")

	resourceName := os.Args[1]
	sch := g.Provider().(*schema.Provider).ResourcesMap[resourceName].Schema
	// log.Printf("%+v\n", sch)

	outputFileName := strings.TrimPrefix(resourceName, "google_") + ".go"
	f, err := os.Create(outputFileName)
	defer f.Close()

	result := []*bytes.Buffer{}
	for k, v := range sch {
		if nestedResource, ok := v.Elem.(*schema.Resource); ok {
			schema := Schema{
				Api:      "compute",
				TypeName: camel(k), //"BackendService",
			}

			for fieldName, fieldSchema := range nestedResource.Schema {
				field := Field{
					SchemaFieldName: fieldName,
					ApiFieldName:    camel(fieldName),
					Type:            parseType(fieldSchema.Type.String()),
				}
				if fieldSchema.Required {
					schema.ReqFields = append(schema.ReqFields, field)
				} else if fieldSchema.Optional {
					schema.OptFields = append(schema.OptFields, field)
				}
			}

			expander := bytes.NewBuffer([]byte{})
			tpl := expanderOutline
			if v.MaxItems != 1 {
				tpl = expanderOutlinePlural
			}
			err := template.Must(template.New("expander").Parse(tpl)).Execute(expander, schema)
			if err != nil {
				log.Fatalf("error with expander template for %s: %s", k, err)
			}
			result = append(result, expander)
		}
	}

	fmtd, err := format.Source(result[0].Bytes())
	if err != nil {
		log.Printf("Formatting error: %s", err)
		fmtd = result[0].Bytes()
	}

	if _, err := f.Write(fmtd); err != nil {
		log.Fatal(err)
	}

	// fileName := os.Args[1]

	// fs := token.NewFileSet()
	// parsedFile, err := parser.ParseFile(fs, fileName, nil, 0)
	// if err != nil {
	// 	log.Fatalf("parsing file %s: %s", fileName, err)
	// }

	// resName := strings.TrimPrefix(fileName, "resource_compute_")
	// resName = strings.TrimSuffix(resName, ".go")
	// outputFileName := fmt.Sprintf("%s_%s.go", "compute", resName)
	// f, err := os.Create(outputFileName)
	// defer f.Close()
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// result := []*bytes.Buffer{}
	// for _, v := range parsedFile.Scope.Objects {
	// 	s := v.Decl.(*ast.FuncDecl).Type.Results.List
	// 	if len(s) != 1 {
	// 		continue
	// 	}
	// 	if fmt.Sprintf("%T", s[0].Type) != "*ast.StarExpr" {
	// 		continue
	// 	}
	// 	l := s[0].Type.(*ast.StarExpr)
	// 	x := l.X.(*ast.SelectorExpr)

	// 	if !(x.X.(*ast.Ident).Name == "schema" && x.Sel.Name == "Resource") {
	// 		continue
	// 	}

	// 	result = parseResourceFn(v.Decl.(*ast.FuncDecl).Body)
	// 	break

	// 	// fmt.Println()
	// }

	// fmtd, err := format.Source(result[0].Bytes())
	// if err != nil {
	// 	log.Printf("Formatting error: %s", err)
	// 	fmtd = result[0].Bytes()
	// }

	// if _, err := f.Write(fmtd); err != nil {
	// 	log.Fatal(err)
	// }
}

func parseResourceFn(resourceFn *ast.BlockStmt) []*bytes.Buffer {
	result := []*bytes.Buffer{}

	if len(resourceFn.List) != 1 {
		log.Fatalf("Found too many statements in function")
	}
	stmt := resourceFn.List[0]

	if len(stmt.(*ast.ReturnStmt).Results) != 1 {
		log.Fatal("Found wrong number of return statements")
	}
	res := stmt.(*ast.ReturnStmt).Results[0]

	// Look for "Schema" field in struct
	for _, el := range res.(*ast.UnaryExpr).X.(*ast.CompositeLit).Elts {
		kve := el.(*ast.KeyValueExpr)
		if kve.Key.(*ast.Ident).Name != "Schema" {
			continue
		}

		result = append(result, parseSchema(kve.Value.(*ast.CompositeLit))...)
	}

	return result
}

func parseSchema(schemaMap *ast.CompositeLit) []*bytes.Buffer {
	result := []*bytes.Buffer{}

	for _, el := range schemaMap.Elts {
		key, _ := strconv.Unquote(el.(*ast.KeyValueExpr).Key.(*ast.BasicLit).Value)
		// fmt.Printf("Key : %s\n", key)

		val := el.(*ast.KeyValueExpr).Value.(*ast.UnaryExpr).X.(*ast.CompositeLit)
		// sc := &schema.Schema{}
		// reqFields := map[string]Field{}
		// optFields := map[string]Field{}
		// unknownFields := map[string]Field{}

		listOrSet := false
		plural := true
		nestedProperties := []ast.Expr{}
		for _, elt := range val.Elts {

			kv := elt.(*ast.KeyValueExpr)
			if kv.Key.(*ast.Ident).Name == "Type" {
				typ := kv.Value.(*ast.SelectorExpr).Sel.Name
				if typ == "TypeList" || typ == "TypeSet" {
					listOrSet = true
				}
			}
			if kv.Key.(*ast.Ident).Name == "Elem" {
				elem := kv.Value.(*ast.UnaryExpr).X.(*ast.CompositeLit)
				typ := elem.Type
				if typ.(*ast.SelectorExpr).Sel.Name == "Resource" {
					kv := elt.(*ast.KeyValueExpr)
					elemVal := kv.Value.(*ast.UnaryExpr).X
					// fmt.Printf("%T %+v\n", elemVal, elemVal)
					propsList := elemVal.(*ast.CompositeLit).Elts
					if len(propsList) != 1 {
						log.Fatalf("Surprising number of nested properties in Elem for key %s", key)
					}
					nestedProperties = propsList[0].(*ast.KeyValueExpr).Value.(*ast.CompositeLit).Elts
				}
				// fmt.Printf("%T %+v\n", typ, typ)
				// for _, elt := range elem.Elts {
				// 	fmt.Printf("%T %+v\n", elt, elt)

			}
			if kv.Key.(*ast.Ident).Name == "MaxItems" {
				if kv.Value.(*ast.BasicLit).Value == "1" {
					plural = false
				}
			}
			// switch kv.Key.(*ast.Ident).Name {
			// case "Type":
			// 	sc.Type = parseType(kv.Value.(*ast.SelectorExpr).Sel.Name)
			// case "Optional":
			// 	b, _ := strconv.ParseBool(kv.Value.(*ast.Ident).Name)
			// 	sc.Optional = b
			// case "Required":
			// 	b, _ := strconv.ParseBool(kv.Value.(*ast.Ident).Name)
			// 	sc.Required = b
			// case "Computed":
			// 	b, _ := strconv.ParseBool(kv.Value.(*ast.Ident).Name)
			// 	sc.Computed = b
			// 	// case "Default":
			// 	// 	sc.Default = parseDefault(kv.Value.(*ast.BasicLit))
			// }
			// fmt.Printf("%T %+v\n", kv.Value, kv.Value)

		}
		if listOrSet && len(nestedProperties) > 0 {
			result = append(result, parseNestedSchema(key, nestedProperties, plural))
		}
		// schemas[key] = sc

		// if key == "backend" {
		// 	fmt.Println(expanderBodyBeginning("*compute.Backend", sc))
		// }
		// fmt.Println()
	}
	// fmt.Printf("Schemas: %+v", schemas)

	return result
}

func parseNestedSchema(key string, props []ast.Expr, plural bool) *bytes.Buffer {
	fmt.Printf("Parsing nested schema %s %v\n", key, plural)

	schema := Schema{
		Api:      "compute",
		TypeName: "BackendService",
	}

	// Key = Schema
	for _, schemaProp := range props {
		kv := schemaProp.(*ast.KeyValueExpr)

		// fmt.Printf("%T %+v\n", kv.Key, kv.Key)
		// fmt.Printf("%T %+v\n", kv.Value, kv.Value)

		fieldName, _ := strconv.Unquote(kv.Key.(*ast.BasicLit).Value)
		field := Field{
			SchemaFieldName: fieldName,
			ApiFieldName:    camel(fieldName),
		}

		req := false
		opt := false

		for _, elementProp := range kv.Value.(*ast.UnaryExpr).X.(*ast.CompositeLit).Elts {
			fmt.Printf("%T %+v\n", elementProp, elementProp)
			kv := elementProp.(*ast.KeyValueExpr)

			switch kv.Key.(*ast.Ident).Name {
			case "Type":
				field.Type = parseType(kv.Value.(*ast.SelectorExpr).Sel.Name)
			case "Required":
				req, _ = strconv.ParseBool(kv.Value.(*ast.Ident).Name)
			case "Optional":
				opt, _ = strconv.ParseBool(kv.Value.(*ast.Ident).Name)
			}
		}

		if req {
			schema.ReqFields = append(schema.ReqFields, field)
		} else if opt {
			schema.OptFields = append(schema.OptFields, field)
		}
		// if kv.Key.(*ast.Ident).Name == "MaxItems" {
		// 	// val := kv.Value.(*ast.SelectorExpr).Sel.
		// }
	}

	// body := bytes.NewBuffer([]byte{})
	// err := template.Must(template.New("inside").Parse(inside)).Execute(body, schema)
	// if err != nil {
	// 	log.Fatalf("error with body template for %s: %s", key, err)
	// }

	// schema.Body = fmt.Sprintf("%s", body.Bytes())

	expander := bytes.NewBuffer([]byte{})
	tpl := expanderOutline
	if plural {
		tpl = expanderOutlinePlural
	}
	err := template.Must(template.New("expander").Parse(tpl)).Execute(expander, schema)
	if err != nil {
		log.Fatalf("error with expander template for %s: %s", key, err)
	}

	// fmt.Printf("%s\n", expander.Bytes())
	return expander
}

func camel(name string) string {
	parts := strings.Split(name, "_")
	for i, v := range parts {
		parts[i] = strings.ToUpper(v[0:1]) + v[1:]
	}
	return strings.Join(parts, "")
}

func parseType(t string) string {
	switch t {
	case "TypeBool":
		return "bool"
	case "TypeInt":
		return "int"
	case "TypeFloat":
		return "float"
	case "TypeString":
		return "string"
	case "TypeList":
		return "list"
	case "TypeMap":
		return "map"
	case "TypeSet":
		return "set"
	}
	return "invalid"
}

// func parseType(t string) schema.ValueType {
// 	switch t {
// 	case "TypeBool":
// 		return schema.TypeBool
// 	case "TypeInt":
// 		return schema.TypeInt
// 	case "TypeFloat":
// 		return schema.TypeFloat
// 	case "TypeString":
// 		return schema.TypeString
// 	case "TypeList":
// 		return schema.TypeList
// 	case "TypeMap":
// 		return schema.TypeMap
// 	case "TypeSet":
// 		return schema.TypeSet
// 	}
// 	return schema.TypeInvalid
// }

type Field struct {
	ApiFieldName    string
	SchemaFieldName string
	Type            string
}

type Schema struct {
	Api       string
	TypeName  string
	ReqFields []Field
	OptFields []Field
	Body      string
}

var expanderOutline string = fmt.Sprintf(`
func expand{{.TypeName}}(configured interface{}) *{{.Api}}.{{.TypeName}} {
	raw := configured.([]interface{})[0].(map[string]interface{})

	%s

	return obj
}`, inside)

var expanderOutlinePlural string = fmt.Sprintf(`
func expand{{.TypeName}}(configured interface{}) []*{{.Api}}.{{.TypeName}} {
	confs := configured.([]interface{})
	result := make([]*{{.Api}}.{{.TypeName}}, 0, len(confs))

	for _, rawIface := range confs {
		raw := rawIface.(map[string]interface{})
		
		%s

		append(result, obj)
	}

	return result
}`, inside)

// var inside string = `
// obj := &{{.Api}}.{{.TypeName}} {
// 	{{range .ReqFields}}{{.ApiFieldName}}: {{if .Type eq "int"}}int64({{end}}raw["{{.SchemaFieldName}}"].({{.Type}}){{if .Type eq "int"}}){{end}}
// 	{{end}}
// }

// {{range .OptFields}}
// if v, ok := raw["{{.SchemaFieldName}}"]; ok {
// 	obj.{{.ApiFieldName}} = {{if .Type eq "int"}}int64({{end}}v.({{.Type}}){{if .Type eq "int"}}){{end}}
// }
// {{end}}`
var inside string = `
obj := &{{.Api}}.{{.TypeName}} {
	{{range .ReqFields}}{{.ApiFieldName}}: raw["{{.SchemaFieldName}}"].({{.Type}})
	{{end}}
}

{{range .OptFields}}
if v, ok := raw["{{.SchemaFieldName}}"]; ok {
	obj.{{.ApiFieldName}} = v.({{.Type}})
}
{{end}}`
