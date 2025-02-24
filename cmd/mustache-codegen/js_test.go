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

func TestCompileJS(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Cannot find node:", err)
	}

	for _, suiteName := range suiteNames {
		t.Run(strings.TrimPrefix(suiteName, "~"), func(t *testing.T) {
			suite, err := loadTestSuite(suiteName)
			if err != nil {
				t.Fatal(err)
			}

			for _, test := range suite {
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

// FuzzCompileJSDeterminism verifies that JavaScript code generation
// yields the same code each time it is called with the same template.
func FuzzCompileJSDeterminism(f *testing.F) {
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
		got1, err := compileJS(s, load)
		if err != nil {
			t.Skip("Invalid template:", err)
		}
		got2, err := compileJS(s, load)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got1, got2) {
			t.Errorf("not deterministic!\n// first:\n%s\n\n// second:\n%s", got1, got2)
		}
	})
}
