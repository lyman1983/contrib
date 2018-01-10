// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/shurcooL/graphql/ident"
)

func main() {
	flag.Parse()

	err := run()
	if err != nil {
		log.Fatalln(err)
	}
}

func run() error {
	githubToken, ok := os.LookupEnv("GITHUB_TOKEN")
	if !ok {
		return fmt.Errorf("GITHUB_TOKEN environment variable not set")
	}
	schema, err := loadSchema(githubToken)
	if err != nil {
		return err
	}

	for filename, t := range templates {
		var buf bytes.Buffer
		err := t.Execute(&buf, schema)
		if err != nil {
			return err
		}
		out, err := format.Source(buf.Bytes())
		if err != nil {
			log.Println(err)
			out = []byte("// gofmt error: " + err.Error() + "\n\n" + buf.String())
		}
		fmt.Println("writing", filename)
		err = ioutil.WriteFile(filename, out, 0644)
		if err != nil {
			return err
		}
	}

	return nil
}

func loadSchema(githubToken string) (schema interface{}, err error) {
	req, err := http.NewRequest("GET", "https://api.github.com/graphql", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "bearer "+githubToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&schema)
	return schema, err
}

// Filename -> Template.
var templates = map[string]*template.Template{
	"enum.go": t(`// Code generated by gen.go; DO NOT EDIT.

package githubql
{{range .data.__schema.types | sortByName}}{{if and (eq .kind "ENUM") (not (internal .name))}}
{{template "enum" .}}
{{end}}{{end}}


{{- define "enum" -}}
// {{.name}} {{.description | endSentence}}
type {{.name}} string

// {{.description | fullSentence}}
const ({{range .enumValues}}
	{{$.name}}{{.name | enumIdentifier}} {{$.name}} = {{.name | quote}} // {{.description | fullSentence}}{{end}}
)
{{- end -}}
`),

	"input.go": t(`// Code generated by gen.go; DO NOT EDIT.

package githubql

// Input represents one of the Input structs:
//
// {{join (inputObjects .data.__schema.types) ", "}}.
type Input interface{}
{{range .data.__schema.types | sortByName}}{{if eq .kind "INPUT_OBJECT"}}
{{template "inputObject" .}}
{{end}}{{end}}


{{- define "inputObject" -}}
// {{.name}} {{.description | endSentence}}
type {{.name}} struct {{"{"}}{{range .inputFields}}{{if eq .type.kind "NON_NULL"}}
	// {{.description | fullSentence}} (Required.)
	{{.name | identifier}} {{.type | type}} ` + "`" + `json:"{{.name}}"` + "`" + `{{end}}{{end}}
{{range .inputFields}}{{if ne .type.kind "NON_NULL"}}
	// {{.description | fullSentence}} (Optional.)
	{{.name | identifier}} {{.type | type}} ` + "`" + `json:"{{.name}},omitempty"` + "`" + `{{end}}{{end}}
}
{{- end -}}
`),
}

func t(text string) *template.Template {
	// typeString returns a string representation of GraphQL type t.
	var typeString func(t map[string]interface{}) string
	typeString = func(t map[string]interface{}) string {
		switch t["kind"] {
		case "NON_NULL":
			s := typeString(t["ofType"].(map[string]interface{}))
			if !strings.HasPrefix(s, "*") {
				panic(fmt.Errorf("nullable type %q doesn't begin with '*'", s))
			}
			return s[1:] // Strip star from nullable type to make it non-null.
		case "LIST":
			return "*[]" + typeString(t["ofType"].(map[string]interface{}))
		default:
			return "*" + t["name"].(string)
		}
	}

	return template.Must(template.New("").Funcs(template.FuncMap{
		"internal": func(s string) bool { return strings.HasPrefix(s, "__") },
		"quote":    strconv.Quote,
		"join":     strings.Join,
		"sortByName": func(types []interface{}) []interface{} {
			sort.Slice(types, func(i, j int) bool {
				ni := types[i].(map[string]interface{})["name"].(string)
				nj := types[j].(map[string]interface{})["name"].(string)
				return ni < nj
			})
			return types
		},
		"inputObjects": func(types []interface{}) []string {
			var names []string
			for _, t := range types {
				t := t.(map[string]interface{})
				if t["kind"].(string) != "INPUT_OBJECT" {
					continue
				}
				names = append(names, t["name"].(string))
			}
			sort.Strings(names)
			return names
		},
		"identifier":     func(name string) string { return ident.ParseLowerCamelCase(name).ToMixedCaps() },
		"enumIdentifier": func(name string) string { return ident.ParseScreamingSnakeCase(name).ToMixedCaps() },
		"type":           typeString,
		"endSentence": func(s string) string {
			s = strings.ToLower(s[0:1]) + s[1:]
			switch {
			default:
				s = "represents " + s
			case strings.HasPrefix(s, "autogenerated "):
				s = "is an " + s
			case strings.HasPrefix(s, "specifies "):
				// Do nothing.
			}
			if !strings.HasSuffix(s, ".") {
				s += "."
			}
			return s
		},
		"fullSentence": func(s string) string {
			if !strings.HasSuffix(s, ".") {
				s += "."
			}
			return s
		},
	}).Parse(text))
}
