package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var suiteNames = []string{
	"Interpolation",
	"Sections",
	"Comments",
	"Inverted",
	"Partials",
	"Delimiters",
	"~Inheritance",
}

type testCase struct {
	Name     string
	Data     json.RawMessage
	Template string
	Partials map[string]string
	Expected string
}

func loadTestSuite(suiteName string) ([]*testCase, error) {
	jsonData, err := os.ReadFile(filepath.Join("testdata", strings.ToLower(suiteName)+".json"))
	if err != nil {
		return nil, err
	}
	var suite struct {
		Tests []*testCase
	}
	if err := json.Unmarshal(jsonData, &suite); err != nil {
		return nil, err
	}
	// Rename tests to UpperCamelCase and strip non-alphanumeric characters.
	for _, test := range suite.Tests {
		words := strings.FieldsFunc(test.Name, func(c rune) bool {
			return !('A' <= c && c <= 'Z' ||
				'a' <= c && c <= 'z' ||
				'0' <= c && c <= '9')
		})
		for i, w := range words {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		test.Name = strings.Join(words, "")
	}
	return suite.Tests, nil
}

func FuzzParse(f *testing.F) {
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
		// Testing to see if parse panics or infinite loops.
		parse(s)
	})
}
