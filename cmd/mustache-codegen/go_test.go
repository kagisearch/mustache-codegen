// Copyright (c) 2025 Kagi Search
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileGo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping for -short")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Cannot find go(?!):", err)
	}

	overrides := map[string]string{
		// Go's html.EscapeString function uses &#34; instead of &quot; because it's shorter.
		// (See https://cs.opensource.google/go/go/+/refs/tags/go1.23.4:src/html/escape.go;l=171)
		// Ideally our tests would normalize the HTML while comparing,
		// but for now, we override the golden value to use the entity escape.
		"Interpolation/HTMLEscaping":                  "These characters should be HTML escaped: &amp; &#34; &lt; &gt;\n",
		"Interpolation/ImplicitIteratorsHTMLEscaping": "These characters should be HTML escaped: &amp; &#34; &lt; &gt;\n",
		"Sections/ImplicitIteratorHTMLEscaping":       "\"(&amp;)(&#34;)(&lt;)(&gt;)\"",
	}

	for _, suiteName := range suiteNames {
		t.Run(strings.TrimPrefix(suiteName, "~"), func(t *testing.T) {
			suite, err := loadTestSuite(suiteName)
			if err != nil {
				t.Fatal(err)
			}

			for _, test := range suite {
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
						"import (\"bytes\"; \"encoding/json\"; \"os\")\n" +
						"func main() {\n" +
						"var data any\n" +
						"if err := json.NewDecoder(os.Stdin).Decode(&data); err != nil { panic(err) }\n" +
						"buf := new(bytes.Buffer)\n" +
						templateName + "(buf, data)\n" +
						"os.Stdout.Write(buf.Bytes())\n" +
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

// FuzzCompileGoDeterminism verifies that Go code generation
// yields the same code each time it is called with the same template.
func FuzzCompileGoDeterminism(f *testing.F) {
	for _, suiteName := range suiteNames {
		suite, err := loadTestSuite(suiteName)
		if err != nil {
			f.Error(err)
		}
		for _, test := range suite {
			f.Add(test.Template)
		}
	}

	f.Fuzz(func(t *testing.T, s string) {
		load := func(name string) (string, error) { return "", nil }
		got1, err := compileGo("foo", "bar", s, load)
		if err != nil {
			t.Skip("Invalid template:", err)
		}
		got2, err := compileGo("foo", "bar", s, load)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got1, got2) {
			t.Errorf("not deterministic!\n// first:\n%s\n\n// second:\n%s", got1, got2)
		}
	})
}
