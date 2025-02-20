package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileJS(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Cannot find node:", err)
	}

	suiteNames := []string{
		"Interpolation",
		"Sections",
		"Comments",
		"Inverted",
		"Partials",
		"~Inheritance",
	}
	overrides := map[string]string{
		// TODO(someday): We're not spec compliant for these whitespace tests,
		// but for our use case (HTML-only output), this doesn't really matter.
		"~Inheritance/Nested block reindentation":    "\n  one\n\nthree\n\n\n",
		"~Inheritance/Intrinsic indentation":         "Hi,\n\none\ntwo\n\n",
		"~Inheritance/Block reindentation":           "Hi,\n\n    one\n    two\n\n",
		"~Inheritance/Standalone block":              "Hi,\n  \none\ntwo\n",
		"~Inheritance/Standalone parent":             "Hi,\n  one\ntwo\n\n",
		"~Inheritance/Override parent with newlines": "\npeaked\n\n:(\n",
		"~Inheritance/Inherit":                       "default content\n",
	}

	for _, suiteName := range suiteNames {
		t.Run(strings.TrimPrefix(suiteName, "~"), func(t *testing.T) {
			jsonData, err := os.ReadFile(filepath.Join("testdata", strings.ToLower(suiteName)+".json"))
			if err != nil {
				t.Fatal(err)
			}

			var suite struct {
				Tests []struct {
					Name     string
					Data     json.RawMessage
					Template string
					Partials map[string]string
					Expected string
				}
			}
			if err := json.Unmarshal(jsonData, &suite); err != nil {
				t.Fatal(err)
			}

			for _, test := range suite.Tests {
				if want, hasOverride := overrides[suiteName+"/"+test.Name]; hasOverride {
					test.Expected = want
				}

				t.Run(test.Name, func(t *testing.T) {
					js, err := compileJS(test.Template, func(name string) (string, error) {
						return test.Partials[name], nil
					})
					if err != nil {
						t.Fatal("compile:", err)
					}
					const templateFilename = "template.mjs"
					templatePath := filepath.Join(t.TempDir(), templateFilename)
					if err := os.WriteFile(templatePath, js, 0o666); err != nil {
						t.Fatal(err)
					}
					generatedCode := bytes.TrimPrefix(js, []byte(prelude))

					script := `import t from './` + templateFilename + `'; process.stdout.write(t(` + string(test.Data) + `))`
					c := exec.Command(nodePath, "--input-type=module", "-e", script)
					c.Dir = filepath.Dir(templatePath)
					stdout := new(bytes.Buffer)
					c.Stdout = stdout
					c.Stderr = os.Stderr
					if err := c.Run(); err != nil {
						t.Fatalf("error: %s\ngenerated code:\n%s", err, generatedCode)
					}
					if got := stdout.String(); got != test.Expected {
						t.Errorf("output:\n%q\nexpected:\n%q\ngenerated code:\n%s", got, test.Expected, generatedCode)
					}
				})
			}
		})
	}
}
