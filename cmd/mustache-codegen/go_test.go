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

func TestCompileGo(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Cannot find go(?!):", err)
	}
	if os.Getenv("CI") != "" {
		t.Skip("Slow test; skipping for CI")
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
		// Go's html.EscapeString function uses &#34; instead of &quot; because it's shorter.
		// (See https://cs.opensource.google/go/go/+/refs/tags/go1.23.4:src/html/escape.go;l=171)
		// Ideally our tests would normalize the HTML while comparing,
		// but for now, we override the golden value to use the entity escape.
		"Interpolation/HTML Escaping":                      "These characters should be HTML escaped: &amp; &#34; &lt; &gt;\n",
		"Interpolation/Implicit Iterators - HTML Escaping": "These characters should be HTML escaped: &amp; &#34; &lt; &gt;\n",
		"Sections/Implicit Iterator - HTML Escaping":       "\"(&amp;)(&#34;)(&lt;)(&gt;)\"",
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
					const templateName = "MyTemplate"
					goSource, err := compileGo("main", templateName, test.Template, func(name string) (string, error) {
						return test.Partials[name], nil
					})
					if err != nil {
						t.Fatal("compile:", err)
					}
					tempDir := t.TempDir()
					if err := os.WriteFile(filepath.Join(tempDir, "template.go"), goSource, 0o666); err != nil {
						t.Fatal(err)
					}
					const runner = "package main\n" +
						"import (\"encoding/json\"; \"os\"; \"strings\")\n" +
						"func main() {\n" +
						"var data any\n" +
						"if err := json.NewDecoder(os.Stdin).Decode(&data); err != nil { panic(err) }\n" +
						"sb := new(strings.Builder)\n" +
						templateName + "(sb, data)\n" +
						"os.Stdout.WriteString(sb.String())\n" +
						"}\n"
					if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(runner), 0o666); err != nil {
						t.Fatal(err)
					}
					currentDir, err := os.Getwd()
					if err != nil {
						t.Fatal(err)
					}
					goMod := "module foo\n" +
						"require github.com/kagisearch/mustache-codegen v0.1.0\n" +
						"replace github.com/kagisearch/mustache-codegen => " +
						filepath.Dir(filepath.Dir(currentDir)) + "\n"
					if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0o666); err != nil {
						t.Fatal(err)
					}

					c := exec.Command(goPath, "mod", "tidy")
					c.Dir = tempDir
					c.Stderr = os.Stderr
					if err := c.Run(); err != nil {
						t.Fatal("go mod tidy:", err)
					}

					c = exec.Command(goPath, "run", ".")
					c.Dir = tempDir
					c.Stdin = bytes.NewReader(test.Data)
					stdout := new(bytes.Buffer)
					c.Stdout = stdout
					c.Stderr = os.Stderr
					if err := c.Run(); err != nil {
						t.Fatalf("error: %s\ngenerated code:\n%s", err, goSource)
					}
					if got := stdout.String(); got != test.Expected {
						t.Errorf("output:\n%q\nexpected:\n%q\ngenerated code:\n%s", got, test.Expected, goSource)
					}
				})
			}
		})
	}
}
