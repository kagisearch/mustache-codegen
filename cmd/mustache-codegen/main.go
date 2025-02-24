package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

	// indentPoint is a directive used to indicate where block indents should be inserted
	// (i.e. at the beginning of logical lines).
	indentPoint
)

const programName = "mustache-codegen"

func main() {
	generatorName := flag.String("lang", "", "`language` to generate code for (js or go)")
	goPkgName := flag.String("go-package", "main", "Go package `name`")
	outputFile := flag.String("o", "", "output `file`")
	flag.Parse()

	var templateName string
	templateDir := "."
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
	generator := map[string]func(string) ([]byte, error){
		"go": func(source string) ([]byte, error) {
			return compileGo(*goPkgName, templateName, source, load)
		},
		"js": func(source string) ([]byte, error) {
			return compileJS(source, load)
		},
	}[*generatorName]
	if generator == nil {
		fmt.Fprintf(os.Stderr, "%s: unknown -lang=%s\n", programName, *generatorName)
		os.Exit(1)
	}

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
		fmt.Fprintf(os.Stderr, "%s: %v\n", programName, err)
		os.Exit(1)
	}

	output, err := generator(string(input))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", programName, err)
		os.Exit(1)
	}
	if *outputFile == "" {
		_, err = os.Stdout.Write(output)
	} else {
		err = os.WriteFile(*outputFile, output, 0o666)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", programName, err)
		os.Exit(1)
	}
}

const (
	defaultStartDelim = "{{"
	defaultEndDelim   = "}}"
)

func parse(s string) ([]tag, error) {
	type scope struct {
		start      tag
		lineno     int
		slice      *[]tag
		standalone bool
	}

	var result []tag
	startDelim := defaultStartDelim
	endDelim := defaultEndDelim

	stack := []scope{
		{slice: &result},
	}

	lineno := 1
	newScope := func(newTag tag) {
		curr := stack[len(stack)-1].slice
		*curr = append(*curr, newTag)
		stack = append(stack, scope{
			start:  newTag,
			lineno: lineno,
			slice:  &(*curr)[len(*curr)-1].body,
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

	dedent := func(s string) string {
		for _, curr := range stack {
			if curr.start.tt != block {
				continue
			}
			var hasIndent bool
			s, hasIndent = strings.CutPrefix(s, curr.start.indent)
			if !hasIndent {
				break
			}
		}
		return s
	}

	// Process (roughly) one line at a time.
	for len(s) > 0 {
		s = dedent(s)
		eol := indexNextLine(s)

		tagStart := strings.Index(s[:eol], startDelim)
		if tagStart < 0 {
			// Add indent point if there are no tags.
			curr := stack[len(stack)-1].slice
			*curr = append(*curr, tag{tt: indentPoint})
		}

		// Line has one or more tags.
		// Hold off on adding literals until we know whether the line is standalone.
		prevEnd := 0
		for tagStart >= 0 {
			special, key, tagEnd, err := cutTag(s, tagStart, startDelim, endDelim)
			if err != nil {
				return nil, fmt.Errorf("%d: %v", lineno, err)
			}
			if n := strings.Count(s[tagStart:tagEnd], "\n"); n > 0 {
				// Tag spanned multiple lines (comment).
				// Update line position variables.
				lineno += n
				eol = tagEnd + indexNextLine(s[tagEnd:])
			}
			if special != '!' && special != '=' {
				// Non-comments must contain a non-whitespace character sequence.
				if key == "" {
					return nil, fmt.Errorf("%d: empty tag", lineno)
				}
				if i := strings.IndexFunc(key, unicode.IsSpace); i >= 0 {
					return nil, fmt.Errorf("%d: extra words in %s tag", lineno, key[:i])
				}
			}

			// "Standalone" tags are those that have nothing except whitespace
			// before or after them on a line.
			// Such tags are treated as though the leading whitespace the rest of the line were not present.
			// "Standalone pair" tags are those where the whitespace after their closing tag
			// is considered instead of the whitespace after the tag itself.
			leadingText := s[prevEnd:tagStart]
			trailingText := s[tagEnd:eol]
			isParameter := special == '$' && stack[len(stack)-1].start.tt != parent
			isArgument := special == '$' && stack[len(stack)-1].start.tt == parent
			isStandalonePairTag := special == '<' || isParameter
			if isStandalonePairTag {
				i, err := elementEnd(key, s, tagEnd, startDelim, endDelim)
				if err != nil {
					return nil, fmt.Errorf("%d: %v", lineno, err)
				}
				trailingText = s[i : i+indexNextLine(s[i:])]
			}
			isStandalone := prevEnd == 0 &&
				special != 0 && special != '&' &&
				isSpace(leadingText) && isSpace(trailingText)
			ignoreRestOfLine := isStandalone && !isStandalonePairTag

			// Compute indentation.
			var indent string
			switch {
			// Argument tags and standalone parameter tags that clear at the end
			// use the following line's indentation.
			case (isArgument || isParameter && isStandalone) && isSpace(s[tagEnd:eol]):
				indent = lineIndentation(dedent(s[eol:]))
				// Such tags also ignore the newline before their content.
				ignoreRestOfLine = true
			// Standalone tags use the indentation from their line.
			case isStandalone:
				indent = leadingText
			}

			// Add any literal text encountered since the last tag.
			if !isStandalone {
				// If this is the first tag we're processing on the line
				// and it's not the argument block close tag at the start of the line,
				// then add an indent point.
				// The special argument block close tag is necessary
				// because we want "{{$foo}}\n  foo\n{{/foo}}" to be treated as "foo\n".
				// If we added an insert point, then we would add an extra indent after the newline
				// during template execution.
				if prevEnd == 0 && !(tagStart == 0 && special == '/' && stack[len(stack)-1].start.tt == block && stack[len(stack)-2].start.tt == parent) {
					curr := stack[len(stack)-1].slice
					*curr = append(*curr, tag{tt: indentPoint})
				}
				appendLiteral(leadingText)
			}

			switch special {
			case '#':
				// Section.
				newScope(tag{
					tt: section,
					s:  key,
				})
			case '^':
				// Inverted section.
				newScope(tag{
					tt: invertedSection,
					s:  key,
				})
			case '!':
				// Comment.
			case '>':
				// Partial.
				curr := stack[len(stack)-1].slice
				*curr = append(*curr, tag{
					tt:     partial,
					s:      key,
					indent: indent,
				})
			case '$':
				// Block.
				newScope(tag{
					tt:     block,
					s:      key,
					indent: indent,
				})
				// If there's more content on the line,
				// then add an indent point to act like the beginning of a line.
				curr := stack[len(stack)-1].slice
				if !ignoreRestOfLine {
					*curr = append(*curr, tag{tt: indentPoint})
				}
			case '<':
				// Parent.
				newScope(tag{
					tt:     parent,
					s:      key,
					indent: indent,
				})
				stack[len(stack)-1].standalone = isStandalone
			case '/':
				// Closing tag.
				last := len(stack) - 1
				if last == 0 {
					return nil, fmt.Errorf("%d: %s/%s%s without opening", lineno, startDelim, key, endDelim)
				}
				if stack[last].start.tt == parent {
					// We already computed whether the closing tag clears when we opened the tag.
					ignoreRestOfLine = stack[last].standalone
				}
				if want := stack[last].start.s; key != want {
					return nil, fmt.Errorf("%d: mismatched %s/%s%s (last opened %s on line %d)",
						lineno, startDelim, key, endDelim, want, stack[last].lineno)
				}
				stack[last] = scope{}
				stack = stack[:last]
			case '=':
				// Set delimiter tag.
				var err error
				startDelim, endDelim, err = splitSetDelimiterTag(key)
				if err != nil {
					return nil, fmt.Errorf("%d: %v", lineno, err)
				}
			case '&':
				// Raw variable.
				curr := stack[len(stack)-1].slice
				*curr = append(*curr, tag{
					tt: rawVariable,
					s:  key,
				})
			default:
				// Escaped variable.
				curr := stack[len(stack)-1].slice
				*curr = append(*curr, tag{
					tt: variable,
					s:  key,
				})
			}

			// Move to next tag in the line.
			if ignoreRestOfLine {
				prevEnd = eol
				break
			}
			prevEnd = tagEnd
			tagStart = nextIndex(s[:eol], tagEnd, startDelim)
		}

		// After we've processed all the tags in the line,
		// add any remaining text as a literal
		// and move on to the next line.
		appendLiteral(s[prevEnd:eol])
		s = s[eol:]
		lineno++
	}

	// Return an error if there are open tags.
	if i := len(stack) - 1; i > 0 {
		last := stack[i]
		return nil, fmt.Errorf("%d: unclosed %s", last.lineno, last.start.s)
	}

	return result, nil
}

// cutTag parses the tag that starts at the index tagStart in s.
// It is assumed that strings.HasPrefix(s[tagStart:], startDelim) reports true.
func cutTag(s string, tagStart int, startDelim, endDelim string) (b byte, key string, tagEnd int, err error) {
	// Find end of tag.
	tagInnerStart := tagStart + len(startDelim)
	isComment := strings.HasPrefix(s[tagInnerStart:], "!")
	tagInnerEnd := tagInnerStart
	tagEnd = -1
	for ; tagInnerEnd+len(endDelim) <= len(s); tagInnerEnd++ {
		if s[tagInnerEnd] == '\n' && !isComment {
			// Newlines only permitted in comments.
			return 0, "", -1, errors.New("unclosed tag")
		}
		if i := tagInnerEnd + len(endDelim); s[tagInnerEnd:i] == endDelim {
			tagEnd = i
			break
		}
	}
	if tagEnd < 0 {
		if isComment {
			return 0, "", -1, errors.New("unclosed comment")
		}
		return 0, "", -1, errors.New("unclosed tag")
	}

	// Check for triple-bracketed (raw) variable.
	// {{{foo}}} is treated identically to {{&foo}}.
	isDefault := startDelim == "{{" && endDelim == "}}"
	if isDefault && s[tagInnerStart] == '{' && strings.HasPrefix(s[tagEnd:], "}") {
		tagInnerStart++
		tagEnd++
		return '&', strings.TrimSpace(s[tagInnerStart:tagInnerEnd]), tagEnd, nil
	}

	// Extract first character if it's one of the known specials.
	inner := s[tagInnerStart:tagInnerEnd]
	if inner, isDelimiter := strings.CutPrefix(inner, "="); isDelimiter {
		inner, hasFinalEquals := strings.CutSuffix(inner, "=")
		if !hasFinalEquals {
			return '=', inner, tagEnd, fmt.Errorf("%s does not end with =%s", s[tagStart:tagEnd], endDelim)
		}
		return '=', strings.TrimSpace(inner), tagEnd, nil
	}
	if len(inner) > 1 && strings.IndexByte(`#^!<>$/&`, inner[0]) >= 0 {
		b = inner[0]
		inner = inner[1:]
	}
	return b, strings.TrimSpace(inner), tagEnd, nil
}

// splitSetDelimiterTag splits the inner content of a set delimiter tag
// into the start and end delimiters.
func splitSetDelimiterTag(s string) (startDelim, endDelim string, err error) {
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i == 0 {
		return "", "", errors.New("set delimiter tag empty")
	}
	if i < 0 {
		return "", "", errors.New("set delimiter tag missing an end delimiter")
	}
	j := nextIndexFunc(s, i, isNonSpace)
	if j < 0 {
		return "", "", errors.New("set delimiter tag missing an end delimiter")
	}
	if k := nextIndexFunc(s, j, unicode.IsSpace); k >= 0 {
		return "", "", errors.New("set delimiter tag has more than two delimiters")
	}
	return s[:i], s[j:], nil
}

// elementEnd returns the end of the matching end tag.
// The search starts at tagEnd,
// the index in s of the end of the start tag with the given name.
func elementEnd(name string, s string, tagEnd int, startDelim, endDelim string) (int, error) {
	level := 1
	i := tagEnd
	for level > 0 {
		tagStart := nextIndex(s, i, startDelim)
		if tagStart < 0 {
			return -1, fmt.Errorf("unclosed %s", name)
		}
		special, key, tagEnd, err := cutTag(s, tagStart, startDelim, endDelim)
		if err != nil {
			return -1, fmt.Errorf("unclosed %s", name)
		}
		switch special {
		case '#', '^', '$', '<':
			if key == name {
				level++
			}
		case '/':
			if key == name {
				level--
			}
		case '=':
			var err error
			startDelim, endDelim, err = splitSetDelimiterTag(key)
			if err != nil {
				return 0, err
			}
		}
		i = tagEnd
	}
	return i, nil
}

// walkTags returns an iterator that visits all tags in the given slice in pre-order.
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

// nextIndex is like [strings.Index],
// but takes in a starting index.
// nextIndex will not return a value less than start
// unless substr is not found within s[start:],
// in which case nextIndex will return -1.
func nextIndex(s string, start int, substr string) int {
	i := strings.Index(s[start:], substr)
	if i < 0 {
		return i
	}
	return start + i
}

// nextIndexFunc is like [strings.IndexFunc],
// but takes in a starting index.
// nextIndexFunc will not return a value less than start
// unless substr is not found within s[start:],
// in which case nextIndexFunc will return -1.
func nextIndexFunc(s string, start int, f func(rune) bool) int {
	i := strings.IndexFunc(s[start:], f)
	if i < 0 {
		return i
	}
	return start + i
}

// indexNextLine returns the index of the last byte of the first line of s (exclusive).
func indexNextLine(s string) int {
	i := strings.IndexByte(s, '\n')
	if i < 0 {
		return len(s)
	}
	return i + 1
}

// lineIndentation returns the longest whitespace-only prefix of line.
func lineIndentation(line string) string {
	firstNonSpace := strings.IndexFunc(line, func(c rune) bool {
		return !unicode.Is(unicode.Zs, c) && c != '\t'
	})
	if firstNonSpace < 0 {
		return line
	}
	return line[:firstNonSpace]
}

// isSpace reports whether all the characters in s are whitespace characters.
func isSpace(s string) bool {
	return !strings.ContainsFunc(s, isNonSpace)
}

func isNonSpace(c rune) bool {
	return !unicode.IsSpace(c)
}
