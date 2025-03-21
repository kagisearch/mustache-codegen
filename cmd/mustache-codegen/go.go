// Copyright (c) 2025 Kagi Search
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	gofmt "go/format"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const supportImportPath = "github.com/kagisearch/mustache-codegen/go/mustache"

func compileGo(packageName string, templateName string, source string, load func(name string) (string, error)) ([]byte, error) {
	tags, err := parse(string(source))
	if err != nil {
		return nil, err
	}

	var partials [][]tag
	partialFuncNames := make(map[string]string)

	var gatherPartials func(tags []tag) error
	gatherPartials = func(tags []tag) error {
		for t := range walkTags(tags) {
			if t.tt == partial || t.tt == parent {
				if partialFuncNames[t.s] != "" {
					continue
				}
				source, err := load(t.s)
				if err != nil {
					return err
				}
				partialTags, err := parse(source)
				if err != nil {
					return fmt.Errorf("partial %s: %v", t.s, err)
				}

				i := slices.IndexFunc(partials, func(p []tag) bool {
					return slices.EqualFunc(partialTags, p, tagsEqual)
				})
				if i == -1 {
					partials = append(partials, partialTags)
					i = len(partials) - 1
				}
				partialFuncNames[t.s] = fmt.Sprintf("_%s_p%d", templateName, i)
				if err := gatherPartials(partialTags); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := gatherPartials(tags); err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	fmt.Fprintln(buf, "// Code generated by mustache-codegen. DO NOT EDIT.")
	fmt.Fprintln(buf)
	fmt.Fprintf(buf, "package %s\n", packageName)
	fmt.Fprintln(buf, "import (")
	fmt.Fprintln(buf, "\t\"bytes\"")
	fmt.Fprintln(buf, "\t\"html\"")
	fmt.Fprintln(buf, "\t\"reflect\"")
	fmt.Fprintln(buf)
	fmt.Fprintf(buf, "\tm %q\n", supportImportPath)
	fmt.Fprintln(buf, ")")

	fmt.Fprintln(buf, "// Ignore unused imports.")
	fmt.Fprintln(buf, "var (")
	fmt.Fprintln(buf, "\t_ = html.EscapeString")
	fmt.Fprintln(buf, "\t_ = reflect.ValueOf")
	fmt.Fprintln(buf, "\t_ = m.Lookup")
	fmt.Fprintln(buf, ")")

	fmt.Fprintf(buf, "\nfunc %s(buf *bytes.Buffer, data any) {\n", lowerSnakeToUpperCamel(templateName))
	fmt.Fprintln(buf, "\tstack := []reflect.Value{reflect.ValueOf(data)}")
	fmt.Fprintln(buf, "\t_ = stack")
	if err := compileTagListGo(buf, tags, partialFuncNames, false, false); err != nil {
		return nil, err
	}
	fmt.Fprintln(buf, "}")

	for i, partialTags := range partials {
		fmt.Fprintf(buf, "\nfunc _%s_p%d(buf *bytes.Buffer, indent string, stack []reflect.Value, blocks map[string]func(*bytes.Buffer, string, []reflect.Value)) {\n", templateName, i)
		if err := compileTagListGo(buf, partialTags, partialFuncNames, true, true); err != nil {
			return nil, err
		}
		fmt.Fprintln(buf, "}")
	}

	formatted, err := gofmt.Source(buf.Bytes())
	if err != nil {
		return nil, err
	}
	return formatted, nil
}

func compileTagListGo(buf *bytes.Buffer, tags []tag, partialFuncNames map[string]string, blocks, indent bool) error {
	for i := 0; i < len(tags); i++ {
		t := tags[i]
		if !indent && t.tt == literal {
			var n int
			t, n = condenseLiteralsWithoutIndentation(tags[i:])
			i += n - 1
		}
		if err := compileTagGo(buf, t, partialFuncNames, blocks, indent); err != nil {
			return err
		}
	}
	return nil
}

func compileTagGo(buf *bytes.Buffer, t tag, partialFuncNames map[string]string, blocks, indent bool) error {
	switch t.tt {
	case literal:
		fmt.Fprintf(buf, "\tbuf.WriteString(%q)\n", t.s)
	case indentPoint:
		if indent {
			fmt.Fprintln(buf, "\tbuf.WriteString(indent)")
		}
	case variable:
		fmt.Fprintf(buf, "\tbuf.WriteString(html.EscapeString(m.ToString(m.Lookup(stack, %q))))\n", t.s)
	case rawVariable:
		fmt.Fprintf(buf, "\tbuf.WriteString(m.ToString(m.Lookup(stack, %q)))\n", t.s)
	case section:
		fmt.Fprintf(buf, "\tfor e := range m.ForEach(m.Lookup(stack, %q)) {\n", t.s)
		fmt.Fprintln(buf, "\t\tstack = append(stack, e)")
		if err := compileTagListGo(buf, t.body, partialFuncNames, blocks, indent); err != nil {
			return err
		}
		fmt.Fprintln(buf, "\t\tclear(stack[len(stack)-1:])")
		fmt.Fprintln(buf, "\t\tstack = stack[:len(stack)-1]")
		fmt.Fprintln(buf, "\t}")
	case invertedSection:
		fmt.Fprintf(buf, "\tif m.IsFalsyOrEmptyList(m.Lookup(stack, %q)) {\n", t.s)
		if err := compileTagListGo(buf, t.body, partialFuncNames, blocks, indent); err != nil {
			return err
		}
		fmt.Fprintln(buf, "\t}")
	case partial:
		fmt.Fprintf(buf, "\t%s(buf, %s, stack, nil)\n", partialFuncNames[t.s], goIncreaseIndent(indent, t.indent))
	case block:
		if blocks {
			fmt.Fprintf(buf, "\tif b, ok := blocks[%q]; ok {\n", t.s)
			fmt.Fprintf(buf, "\t\tb(buf, %s, stack)\n", goIncreaseIndent(t.indentArgument && indent, t.indent))
			fmt.Fprintln(buf, "\t} else {")
		}
		if err := compileTagListGo(buf, t.body, partialFuncNames, blocks, indent); err != nil {
			return err
		}
		if blocks {
			fmt.Fprintln(buf, "\t}")
		}
	case parent:
		fmt.Fprintln(buf, "\t{")
		fmt.Fprintln(buf, "\t\tpartialBlocks := make(map[string]func(*bytes.Buffer, string, []reflect.Value))")
		fmt.Fprintln(buf, "\t\t_ = partialBlocks")
		for _, blockTag := range t.body {
			if blockTag.tt != block {
				continue
			}
			fmt.Fprintf(buf, "\t\tpartialBlocks[%q] = func(buf *bytes.Buffer, indent string, stack []reflect.Value) {\n", blockTag.s)
			if err := compileTagListGo(buf, blockTag.body, partialFuncNames, blocks, true); err != nil {
				return err
			}
			fmt.Fprintln(buf, "\t\t}")
		}
		if blocks {
			fmt.Fprintln(buf, "\t\tfor k, v := range blocks {")
			fmt.Fprintln(buf, "\t\t\tpartialBlocks[k] = v")
			fmt.Fprintln(buf, "\t\t}")
		}
		fmt.Fprintf(buf, "\t\t%s(buf, %s, stack, partialBlocks)\n", partialFuncNames[t.s], goIncreaseIndent(indent, t.indent))
		fmt.Fprintln(buf, "\t}")
	default:
		return fmt.Errorf("unhandled tag %d", t.tt)
	}
	return nil
}

func lowerSnakeToUpperCamel(s string) string {
	sb := new(strings.Builder)
	capitalize := true
	for _, c := range s {
		if capitalize {
			sb.WriteRune(unicode.ToUpper(c))
			capitalize = false
		} else if c == '_' {
			capitalize = true
		} else {
			sb.WriteRune(c)
		}
	}
	return sb.String()
}

func goIndentArg(indent bool) string {
	if indent {
		return "indent"
	} else {
		return `""`
	}
}

func goIncreaseIndent(indentVar bool, indent string) string {
	switch {
	case indentVar && indent == "":
		return "indent"
	case !indentVar:
		return strconv.Quote(indent)
	default:
		return "indent+" + strconv.Quote(indent)
	}
}
