package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
	"unicode"
)

type tag struct {
	tt     tagType
	s      string
	indent string
	body   []tag
}

type tagType int

const (
	literal tagType = iota
	variable
	rawVariable
	section
	invertedSection
	partial
	block
	parent
)

//go:embed prelude.js
var prelude string

func main() {
	goOutput := flag.Bool("go", false, "compile to Go")
	goPkgName := flag.String("package", "main", "Go package `name`")
	flag.Parse()

	var templateName string
	templateDir := "."
	var input []byte
	var err error
	if fname := flag.Arg(0); fname != "" {
		templateName = strings.TrimSuffix(filepath.Base(fname), ".mustache")
		templateDir = filepath.Dir(fname)
		input, err = os.ReadFile(fname)
	} else {
		input, err = io.ReadAll(os.Stdin)
		templateName = "stdin"
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var output []byte
	load := func(name string) (string, error) {
		data, err := os.ReadFile(filepath.Join(templateDir, name+".mustache"))
		if os.IsNotExist(err) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if *goOutput {
		output, err = compileGo(*goPkgName, templateName, string(input), load)
	} else {
		output, err = compileJS(string(input), load)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(output)
}

func compileJS(source string, load func(name string) (string, error)) ([]byte, error) {
	tags, err := parse(string(source))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var partials [][]tag
	sourceCache := make(map[string]string)
	partialFuncNames := make(map[partialKey]string)

	var gatherPartials func(tags []tag) error
	gatherPartials = func(tags []tag) error {
		for t := range walkTags(tags) {
			if t.tt == partial || t.tt == parent {
				k := partialKey{name: t.s, indent: t.indent}
				if partialFuncNames[k] != "" {
					continue
				}
				source, cached := sourceCache[t.s]
				if !cached {
					var err error
					source, err = load(t.s)
					if err != nil {
						return err
					}
				}
				partialTags, err := parse(indent(source, t.indent))
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
				partialFuncNames[k] = fmt.Sprintf("p%d", i)
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
		buf.WriteString(`(s,b){let x=''`)
		for _, t := range partialTags {
			if err := compileTagJS(buf, t, partialFuncNames, true); err != nil {
				return nil, err
			}
		}
		buf.WriteString(`;return x}` + "\n")
	}

	buf.WriteString(`export default function(data){let s=[data],x=''`)
	for _, t := range tags {
		if err := compileTagJS(buf, t, partialFuncNames, false); err != nil {
			return nil, err
		}
	}
	buf.WriteString(`;return x}`)
	return buf.Bytes(), nil
}

type partialKey struct {
	name   string
	indent string
}

func compileTagJS(buf *bytes.Buffer, t tag, partialFuncNames map[partialKey]string, blocks bool) error {
	// prelude helpers:
	// esc(s): escape value
	// f(x): is falsey
	// arr(x): is array
	// ind(tab, s): indent

	// guide to variables:
	// x: output string
	// d: data argument (don't use)
	// s: context stack
	// c: section context
	// g: anonymous block function
	// e: anonymous block function context
	// b: blocks record
	// bb: block

	switch t.tt {
	case literal:
		buf.WriteString(`;x+='`)
		template.JSEscape(buf, []byte(t.s))
		buf.WriteString(`'`)
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
			if err := compileTagJS(buf, tag, partialFuncNames, blocks); err != nil {
				return err
			}
		}
		buf.WriteString(`;s.pop(e)};arr(c)?c.forEach(g):g(c)}}`)
	case invertedSection:
		buf.WriteString(`;if(f(`)
		compileNamePathJS(buf, t.s)
		buf.WriteString(`)){`)
		for _, sub := range t.body {
			if err := compileTagJS(buf, sub, partialFuncNames, blocks); err != nil {
				return err
			}
		}
		buf.WriteString(`}`)
	case partial:
		buf.WriteString(`;x+=`)
		k := partialKey{name: t.s, indent: t.indent}
		buf.WriteString(partialFuncNames[k])
		buf.WriteString(`(s,{})`)
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
			buf.WriteString(`;if(bb!==undefined)x+=bb(s);else{`)
		}
		for _, sub := range t.body {
			if err := compileTagJS(buf, sub, partialFuncNames, blocks); err != nil {
				return err
			}
		}
		if blocks {
			buf.WriteString(`}}`)
		}
	case parent:
		buf.WriteString(`;x+=`)
		k := partialKey{name: t.s, indent: t.indent}
		buf.WriteString(partialFuncNames[k])
		buf.WriteString(`(s,{`)
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
			buf.WriteString(`:(s)=>{let x=''`)
			for _, blockSub := range blockTag.body {
				if err := compileTagJS(buf, blockSub, partialFuncNames, blocks); err != nil {
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

func parse(s string) ([]tag, error) {
	type scope struct {
		name  string
		slice *[]tag
	}

	var result []tag
	startDelim := "{{"
	endDelim := "}}"

	stack := []scope{
		{slice: &result},
	}

	newScope := func(tt tagType, name string) {
		curr := stack[len(stack)-1].slice
		*curr = append(*curr, tag{
			tt: tt,
			s:  name,
		})
		stack = append(stack, scope{
			name:  name,
			slice: &(*curr)[len(*curr)-1].body,
		})
	}

	appendLiteral := func(s string) {
		if s != "" {
			curr := stack[len(stack)-1].slice
			*curr = append(*curr, tag{
				tt: literal,
				s:  s,
			})
		}
	}

	for base, prevStandalone := 0, -1; ; {
		curr := stack[len(stack)-1].slice
		search := s[base:]

		tagStart := strings.Index(search, startDelim)
		if tagStart < 0 {
			appendLiteral(search)
			break
		}

		tagInnerStart := tagStart + len(startDelim)
		tagLength := strings.Index(search[tagInnerStart:], endDelim)
		if tagLength < 0 {
			appendLiteral(search[tagStart:])
			break
		}

		tagInnerEnd := tagInnerStart + tagLength
		tagInner := search[tagInnerStart:tagInnerEnd]
		tagEnd := tagInnerEnd + len(endDelim)
		special, name := cutTag(tagInner, startDelim == "{{" && endDelim == "}}")
		if name == "" {
			appendLiteral(search[:tagEnd])
			base += tagEnd
			continue
		}

		// Literals and whitespace handling.
		var indent string
		switch special {
		case '!', '#', '^', '/', '<', '>', '$':
			if prevLineEnd, nextLineStart, insertNL, lineIndent, ok := isStandalone(s, base+tagStart, base+tagEnd); ok {
				indent = lineIndent
				if base < prevLineEnd {
					appendLiteral(s[base:prevLineEnd])
				}
				if prevLineEnd > prevStandalone {
					appendLiteral(insertNL)
				}
				base = nextLineStart
				prevStandalone = nextLineStart
			} else {
				appendLiteral(search[:tagStart])
				base += tagEnd
			}
		default:
			appendLiteral(search[:tagStart])
			base += tagEnd
		}

		switch special {
		case '#':
			newScope(section, name)
		case '^':
			newScope(invertedSection, name)
		case '!':
			// Comment
		case '>':
			*curr = append(*curr, tag{
				tt:     partial,
				s:      name,
				indent: indent,
			})
		case '$':
			newScope(block, name)
		case '<':
			newScope(parent, name)
		case '/':
			// End
			if len(stack) == 1 {
				return nil, fmt.Errorf("%s/%s%s without opening", startDelim, name, endDelim)
			}
			if want := stack[len(stack)-1].name; name != want {
				return nil, fmt.Errorf("mismatched %s%s%s (last opened %s)", startDelim, name, endDelim, want)
			}
			stack = stack[:len(stack)-1]
		case '&':
			*curr = append(*curr, tag{
				tt: rawVariable,
				s:  name,
			})
		case '{':
			if strings.HasPrefix(s[base:], "}") {
				*curr = append(*curr, tag{
					tt: rawVariable,
					s:  name,
				})
				base++
			} else {
				*curr = append(*curr, tag{
					tt: variable,
					s:  name,
				})
			}
		default:
			*curr = append(*curr, tag{
				tt: variable,
				s:  name,
			})
		}
	}

	if len(stack) > 1 {
		return nil, fmt.Errorf("unclosed %s", stack[len(stack)-1].name)
	}

	return result, nil
}

func cutTag(inner string, isDefault bool) (b byte, name string) {
	if len(inner) > 1 && (strings.IndexByte(`#^!<>$/&`, inner[0]) >= 0 || inner[0] == '{' && isDefault) {
		b = inner[0]
		inner = inner[1:]
	}
	return b, strings.TrimSpace(inner)
}

func isStandalone(s string, tagStart, tagEnd int) (prevLineEnd, nextLineStart int, insert, indent string, ok bool) {
	i := strings.LastIndex(s[:tagStart], "\n")
	var lineStart int
	if i == -1 {
		lineStart = 0
		prevLineEnd = 0
	} else {
		lineStart = i + 1
		prevLineEnd = i
		if strings.HasSuffix(s[:prevLineEnd], "\r") {
			prevLineEnd--
			insert = "\r\n"
		} else {
			insert = "\n"
		}
	}
	nonSpace := func(c rune) bool { return !unicode.IsSpace(c) }
	i = strings.Index(s[tagEnd:], "\n")
	var lineEnd int
	if i == -1 {
		lineEnd = len(s)
		nextLineStart = len(s)
	} else {
		lineEnd = tagEnd + i
		nextLineStart = tagEnd + i + 1
	}

	if strings.ContainsFunc(s[lineStart:tagStart], nonSpace) || strings.ContainsFunc(s[tagEnd:lineEnd], nonSpace) {
		return 0, 0, "", "", false
	}

	return prevLineEnd, nextLineStart, insert, s[lineStart:tagStart], true
}

func walkTags(tags []tag) iter.Seq[tag] {
	var walk func(tags []tag, yield func(tag) bool) bool
	walk = func(tags []tag, yield func(tag) bool) bool {
		for _, t := range tags {
			if !yield(t) {
				return false
			}
			if !walk(t.body, yield) {
				return false
			}
		}
		return true
	}
	return func(yield func(tag) bool) {
		walk(tags, yield)
	}
}

func tagsEqual(t1, t2 tag) bool {
	if t1.tt != t2.tt || t1.s != t2.s || t1.indent != t2.indent {
		return false
	}
	return slices.EqualFunc(t1.body, t2.body, tagsEqual)
}

func indent(s, indent string) string {
	if indent == "" {
		return s
	}
	sb := new(strings.Builder)
	sb.Grow(len(s) + len(indent))
	for {
		eol := strings.IndexByte(s, '\n')
		var line string
		if eol == -1 {
			line = s
		} else {
			eol++
			line = s[:eol]
		}
		sb.WriteString(indent)
		sb.WriteString(line)
		if eol == -1 || eol == len(s) {
			break
		}
		s = s[eol:]
	}
	return sb.String()
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
