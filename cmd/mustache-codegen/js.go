package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"text/template"
)

//go:embed prelude.js
var prelude string

func compileJS(source string, load func(name string) (string, error)) ([]byte, error) {
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
				partialFuncNames[t.s] = fmt.Sprintf("p%d", i)
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
	buf.WriteString(prelude)

	for i, partialTags := range partials {
		fmt.Fprintf(buf, "function p%d", i)
		buf.WriteString(`(n,s,b){let x=''`)
		for _, t := range partialTags {
			if err := compileTagJS(buf, t, partialFuncNames, true, true); err != nil {
				return nil, err
			}
		}
		buf.WriteString(`;return x}` + "\n")
	}

	buf.WriteString(`export default function(data){let s=[data],x=''`)
	for _, t := range tags {
		if err := compileTagJS(buf, t, partialFuncNames, false, false); err != nil {
			return nil, err
		}
	}
	buf.WriteString(`;return x}`)
	return buf.Bytes(), nil
}

func compileTagJS(buf *bytes.Buffer, t tag, partialFuncNames map[string]string, blocks, indent bool) error {
	// prelude helpers:
	// esc(s): escape value
	// f(x): is falsey
	// arr(x): is array
	// look(s,k): lookup k in stack s

	// guide to variables:
	// x: output string
	// d: data argument (don't use)
	// s: context stack
	// c: section context
	// g: anonymous block function
	// e: anonymous block function context
	// b: blocks record
	// bb: block
	// n: indent

	switch t.tt {
	case literal:
		buf.WriteString(`;x+='`)
		template.JSEscape(buf, []byte(t.s))
		buf.WriteString(`'`)
	case indentPoint:
		if indent {
			buf.WriteString(";x+=n")
		}
	case variable:
		buf.WriteString(`;x+=esc(`)
		compileNamePathJS(buf, t.s)
		buf.WriteString("??'')")
	case rawVariable:
		buf.WriteString(`;x+=`)
		compileNamePathJS(buf, t.s)
		buf.WriteString("??''")
	case section:
		buf.WriteString(`;{let c=`)
		compileNamePathJS(buf, t.s)
		buf.WriteString(`;if(!f(c)){let g=(e)=>{s.push(e)`)
		for _, tag := range t.body {
			if err := compileTagJS(buf, tag, partialFuncNames, blocks, indent); err != nil {
				return err
			}
		}
		buf.WriteString(`;s.pop(e)};arr(c)?c.forEach(g):g(c)}}`)
	case invertedSection:
		buf.WriteString(`;if(f(`)
		compileNamePathJS(buf, t.s)
		buf.WriteString(`)){`)
		for _, sub := range t.body {
			if err := compileTagJS(buf, sub, partialFuncNames, blocks, indent); err != nil {
				return err
			}
		}
		buf.WriteString(`}`)
	case partial:
		buf.WriteString(`;x+=`)
		buf.WriteString(partialFuncNames[t.s])
		buf.WriteString("(")
		jsIncreaseIndent(buf, indent, t.indent)
		buf.WriteString(`,s,{})`)
	case block:
		if blocks {
			buf.WriteString(`;{const bb=b`)
			if isJSIdentifier(t.s) {
				buf.WriteString(`.`)
				buf.WriteString(t.s)
			} else {
				buf.WriteString(`['`)
				template.JSEscape(buf, []byte(t.s))
				buf.WriteString(`']`)
			}
			buf.WriteString(`;if(bb!==undefined)x+=bb(`)
			jsIncreaseIndent(buf, indent, t.indent)
			buf.WriteString(`,s);else{`)
		}
		for _, sub := range t.body {
			if err := compileTagJS(buf, sub, partialFuncNames, blocks, indent); err != nil {
				return err
			}
		}
		if blocks {
			buf.WriteString(`}}`)
		}
	case parent:
		buf.WriteString(`;x+=`)
		buf.WriteString(partialFuncNames[t.s])
		buf.WriteString("(")
		jsIncreaseIndent(buf, indent, t.indent)
		buf.WriteString(`,s,{`)
		first := true
		for _, blockTag := range t.body {
			if blockTag.tt != block {
				continue
			}
			if !first {
				buf.WriteString(`,`)
			}
			if isJSIdentifier(blockTag.s) {
				buf.WriteString(blockTag.s)
			} else {
				buf.WriteString(`'`)
				template.JSEscape(buf, []byte(blockTag.s))
				buf.WriteString(`'`)
			}
			buf.WriteString(`:(n,s)=>{let x=''`)
			for _, blockSub := range blockTag.body {
				if err := compileTagJS(buf, blockSub, partialFuncNames, blocks, true); err != nil {
					return err
				}
			}
			buf.WriteString(`;return x}`)
			first = false
		}
		if blocks {
			if !first {
				buf.WriteString(`,`)
			}
			buf.WriteString(`...b`)
		}
		buf.WriteString(`})`)
	default:
		return fmt.Errorf("unhandled tag %d", t.tt)
	}
	return nil
}

func compileNamePathJS(w *bytes.Buffer, name string) {
	if name == "." {
		w.WriteString("s.at(-1)")
		return
	}

	parts := strings.Split(name, ".")
	w.WriteString("look(s,'")
	template.JSEscape(w, []byte(parts[0]))
	w.WriteString("')")
	for _, part := range parts[1:] {
		w.WriteString("?.")
		if isJSIdentifier(part) {
			w.WriteString(part)
		} else {
			w.WriteString(`['`)
			template.JSEscape(w, []byte(part))
			w.WriteString(`']`)
		}
	}
}

func jsIndentArg(indent bool) string {
	if indent {
		return "n"
	} else {
		return "''"
	}
}

func jsIncreaseIndent(w *bytes.Buffer, indentVar bool, indent string) {
	switch {
	case indentVar && indent == "":
		w.WriteString("n")
	case !indentVar:
		w.WriteString("'")
		template.JSEscape(w, []byte(indent))
		w.WriteString("'")
	default:
		w.WriteString("n+'")
		template.JSEscape(w, []byte(indent))
		w.WriteString("'")
	}
}

func isJSIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	if !isIdentChar(s[0]) {
		return false
	}
	for _, c := range []byte(s[1:]) {
		if !isIdentChar(c) && !isDigit(c) {
			return false
		}
	}
	return true
}

func isIdentChar(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_' || c == '$'
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}
