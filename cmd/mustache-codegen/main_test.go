// Copyright (c) 2025 Kagi Search
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"maps"
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
	"Extra",
}

type testCase struct {
	Name     string
	Data     json.RawMessage
	Template string
	Partials map[string]string
	Expected string
}

var extraSuite = []*testCase{
	{
		Name:     "ParentIndentationWithoutStandaloneParameter",
		Data:     json.RawMessage(`{}`),
		Template: " {{<a}}\n  {{$id}}123{{/id}}\n {{/a}}\n",
		Partials: map[string]string{
			"a": "<div\n id=\"{{$id}}{{/id}}\"\n>hi</div>\n",
		},
		Expected: " <div\n  id=\"123\"\n >hi</div>\n",
	},
	{
		Name:     "ParentIndentationWithMultilineArgumentWithoutStandaloneParameter",
		Data:     json.RawMessage(`{}`),
		Template: " {{<a}}\n  {{$id}}\n  123\n  456\n{{/id}}\n {{/a}}\n",
		Partials: map[string]string{
			"a": "<div\n id=\"{{$id}}{{/id}}\"\n>hi</div>\n",
		},
		Expected: " <div\n  id=\"123\n456\n\"\n >hi</div>\n",
	},
	{
		Name:     "MultilineArgumentWithStandaloneParameter",
		Data:     json.RawMessage(`{}`),
		Template: " {{<a}}\n  {{$b}}\n   123\n   456\n  {{/b}}\n {{/a}}\n",
		Partials: map[string]string{
			"a": "<div>\n {{$b}}{{/b}}\n</div>\n",
		},
		Expected: " <div>\n 123\n 456\n\n </div>\n",
	},
}

func loadTestSuite(suiteName string) ([]*testCase, error) {
	if suiteName == "Extra" {
		// Defensive copy.
		suite := make([]*testCase, 0, len(extraSuite))
		for _, test := range extraSuite {
			testCopy := new(testCase)
			*testCopy = *test
			testCopy.Partials = maps.Clone(test.Partials)
			testCopy.Data = bytes.Clone(testCopy.Data)
			suite = append(suite, testCopy)
		}
		return suite, nil
	}

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
