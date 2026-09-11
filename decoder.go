// Package yamlconfig implements a validating decoder and pretty printer for
// a practical subset of block-style YAML, using only the standard library.
//
// The decoder is token based, in the spirit of encoding/xml.Decoder: callers
// pull one Token at a time instead of getting a fully built tree back. That
// keeps memory use bounded by the depth of the document (the stack of open
// mappings and sequences) rather than by its total size, so validating or
// reformatting a large config never requires reading the whole thing into
// memory first.
package yamlconfig

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// TokenKind identifies the shape of a Token.
type TokenKind int

const (
	MappingStart TokenKind = iota
	MappingEnd
	SequenceStart
	SequenceEnd
	Key
	Scalar
)

func (k TokenKind) String() string {
	switch k {
	case MappingStart:
		return "MappingStart"
	case MappingEnd:
		return "MappingEnd"
	case SequenceStart:
		return "SequenceStart"
	case SequenceEnd:
		return "SequenceEnd"
	case Key:
		return "Key"
	case Scalar:
		return "Scalar"
	default:
		return "Unknown"
	}
}

// Token is one step through a parsed document. Value is set for Key and
// Scalar tokens. Quoted records whether a Scalar came from a quoted string
// in the source, so the printer can preserve that distinction (an empty
// unquoted scalar is null; an empty quoted scalar is the empty string).
type Token struct {
	Kind   TokenKind
	Value  string
	Quoted bool
	Line   int
}

type containerKind int

const (
	kindMapping containerKind = iota
	kindSequence
)

type frame struct {
	indent         int
	kind           containerKind
	expectingValue bool
	keys           map[string]bool
}

// Decoder reads a document from an io.Reader and turns it into a stream of
// Tokens. It reads one line at a time and only ever holds the stack of
// currently open containers, never the whole input.
type Decoder struct {
	r        *bufio.Reader
	line     int
	stack    []*frame
	queue    []Token
	rootSeen bool
	finished bool
}

// NewDecoder returns a Decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

// Token returns the next token in the document, or io.EOF once the document
// is fully consumed.
func (d *Decoder) Token() (Token, error) {
	if d.finished && len(d.queue) == 0 {
		return Token{}, io.EOF
	}
	for len(d.queue) == 0 {
		line, err := d.r.ReadString('\n')
		if err != nil && err != io.EOF {
			return Token{}, err
		}
		if line != "" {
			d.line++
			if perr := d.processLine(line); perr != nil {
				return Token{}, perr
			}
		}
		if err == io.EOF {
			d.finish()
			if len(d.queue) == 0 {
				return Token{}, io.EOF
			}
		}
	}
	tok := d.queue[0]
	d.queue = d.queue[1:]
	return tok, nil
}

func (d *Decoder) finish() {
	if d.finished {
		return
	}
	d.finished = true
	for len(d.stack) > 0 {
		d.closeFrame(d.pop())
	}
}

func (d *Decoder) pop() *frame {
	f := d.stack[len(d.stack)-1]
	d.stack = d.stack[:len(d.stack)-1]
	return f
}

func (d *Decoder) closeFrame(f *frame) {
	if f.expectingValue {
		d.queue = append(d.queue, Token{Kind: Scalar, Line: d.line})
	}
	end := MappingEnd
	if f.kind == kindSequence {
		end = SequenceEnd
	}
	d.queue = append(d.queue, Token{Kind: end, Line: d.line})
}

func (d *Decoder) processLine(raw string) error {
	line := strings.TrimRight(raw, "\r\n")
	if line == "" {
		return nil
	}
	indent, rest, err := splitIndent(line, d.line)
	if err != nil {
		return err
	}
	content := strings.TrimRight(stripComment(rest), " \t")
	if content == "" || content == "---" || content == "..." {
		return nil
	}
	return d.emit(indent, content)
}

func (d *Decoder) emit(indent int, content string) error {
	for len(d.stack) > 0 && indent < d.stack[len(d.stack)-1].indent {
		d.closeFrame(d.pop())
	}

	if len(d.stack) == 0 {
		return d.startRoot(indent, content)
	}

	top := d.stack[len(d.stack)-1]

	if indent > top.indent {
		if !top.expectingValue {
			return fmt.Errorf("yamlconfig: line %d: unexpected indentation", d.line)
		}
		top.expectingValue = false
		return d.startContainer(indent, content)
	}

	if top.expectingValue {
		d.queue = append(d.queue, Token{Kind: Scalar, Line: d.line})
		top.expectingValue = false
	}

	if top.kind == kindSequence {
		return d.emitSequenceItem(top, content)
	}
	return d.emitMappingEntry(top, content)
}

func (d *Decoder) startRoot(indent int, content string) error {
	if d.rootSeen {
		return fmt.Errorf("yamlconfig: line %d: multiple documents are not supported", d.line)
	}
	d.rootSeen = true

	if isSequenceItem(content) {
		f := &frame{indent: indent, kind: kindSequence}
		d.stack = append(d.stack, f)
		d.queue = append(d.queue, Token{Kind: SequenceStart, Line: d.line})
		return d.emitSequenceItem(f, content)
	}
	if key, rawVal, ok := splitMappingEntry(content); ok {
		f := &frame{indent: indent, kind: kindMapping}
		d.stack = append(d.stack, f)
		d.queue = append(d.queue, Token{Kind: MappingStart, Line: d.line})
		return d.applyMappingEntry(f, key, rawVal)
	}

	val, quoted := parseScalar(content)
	d.queue = append(d.queue, Token{Kind: Scalar, Value: val, Quoted: quoted, Line: d.line})
	return nil
}

func (d *Decoder) startContainer(indent int, content string) error {
	if isSequenceItem(content) {
		f := &frame{indent: indent, kind: kindSequence}
		d.stack = append(d.stack, f)
		d.queue = append(d.queue, Token{Kind: SequenceStart, Line: d.line})
		return d.emitSequenceItem(f, content)
	}
	key, rawVal, ok := splitMappingEntry(content)
	if !ok {
		return fmt.Errorf("yamlconfig: line %d: expected a mapping entry or sequence item", d.line)
	}
	f := &frame{indent: indent, kind: kindMapping}
	d.stack = append(d.stack, f)
	d.queue = append(d.queue, Token{Kind: MappingStart, Line: d.line})
	return d.applyMappingEntry(f, key, rawVal)
}

func (d *Decoder) emitMappingEntry(f *frame, content string) error {
	key, rawVal, ok := splitMappingEntry(content)
	if !ok {
		return fmt.Errorf("yamlconfig: line %d: expected \"key: value\"", d.line)
	}
	return d.applyMappingEntry(f, key, rawVal)
}

func (d *Decoder) applyMappingEntry(f *frame, key, rawVal string) error {
	if f.keys == nil {
		f.keys = make(map[string]bool)
	}
	if f.keys[key] {
		return fmt.Errorf("yamlconfig: line %d: duplicate key %q", d.line, key)
	}
	f.keys[key] = true

	d.queue = append(d.queue, Token{Kind: Key, Value: key, Line: d.line})
	if rawVal == "" {
		f.expectingValue = true
		return nil
	}
	val, quoted := parseScalar(rawVal)
	d.queue = append(d.queue, Token{Kind: Scalar, Value: val, Quoted: quoted, Line: d.line})
	return nil
}

func (d *Decoder) emitSequenceItem(f *frame, content string) error {
	if !isSequenceItem(content) {
		return fmt.Errorf("yamlconfig: line %d: expected a sequence item (\"- \")", d.line)
	}
	if content == "-" {
		f.expectingValue = true
		return nil
	}

	rest := content[2:]
	trimmed := strings.TrimLeft(rest, " ")
	itemIndent := f.indent + 2 + (len(rest) - len(trimmed))
	item := trimmed
	if item == "" {
		f.expectingValue = true
		return nil
	}
	if item == "-" || strings.HasPrefix(item, "- ") {
		return fmt.Errorf("yamlconfig: line %d: nested inline sequences are not supported", d.line)
	}

	if key, rawVal, ok := splitMappingEntry(item); ok {
		nf := &frame{indent: itemIndent, kind: kindMapping}
		d.stack = append(d.stack, nf)
		d.queue = append(d.queue, Token{Kind: MappingStart, Line: d.line})
		return d.applyMappingEntry(nf, key, rawVal)
	}

	val, quoted := parseScalar(item)
	d.queue = append(d.queue, Token{Kind: Scalar, Value: val, Quoted: quoted, Line: d.line})
	return nil
}

func isSequenceItem(content string) bool {
	return content == "-" || strings.HasPrefix(content, "- ")
}

func splitIndent(line string, lineNo int) (int, string, error) {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i < len(line) && line[i] == '\t' {
		return 0, "", fmt.Errorf("yamlconfig: line %d: tabs are not allowed for indentation", lineNo)
	}
	return i, line[i:], nil
}

func stripComment(s string) string {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '#':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return s[:i]
			}
		}
	}
	return s
}

// splitMappingEntry splits content into a key and the (still raw, trimmed)
// remainder after the separating colon. ok is false if content is not a
// mapping entry at all.
func splitMappingEntry(content string) (key, rawVal string, ok bool) {
	key, remainder, ok := splitKey(content)
	if !ok {
		return "", "", false
	}
	return key, strings.TrimSpace(remainder), true
}

func splitKey(content string) (key string, remainder string, ok bool) {
	if len(content) > 0 && (content[0] == '"' || content[0] == '\'') {
		q := content[0]
		end := findClosingQuote(content, 0, q)
		if end < 0 {
			return "", "", false
		}
		if q == '"' {
			key = unescapeDouble(content[1:end])
		} else {
			key = strings.ReplaceAll(content[1:end], "''", "'")
		}
		rest := strings.TrimLeft(content[end+1:], " \t")
		if rest == "" || rest[0] != ':' {
			return "", "", false
		}
		return key, rest[1:], true
	}

	idx := indexUnquotedColon(content)
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(content[:idx])
	if key == "" {
		return "", "", false
	}
	return key, content[idx+1:], true
}

// indexUnquotedColon finds the first colon outside of quotes that either
// ends the string or is followed by whitespace, matching the ": " rule
// block-style YAML uses to tell a key/value separator from a plain colon.
func indexUnquotedColon(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case ':':
			if i == len(s)-1 || s[i+1] == ' ' || s[i+1] == '\t' {
				return i
			}
		}
	}
	return -1
}

func findClosingQuote(s string, start int, q byte) int {
	for i := start + 1; i < len(s); i++ {
		if s[i] == q {
			if q == '\'' && i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			return i
		}
	}
	return -1
}

// parseScalar turns raw source text for a scalar value into its decoded
// form and whether it was quoted in the source.
func parseScalar(raw string) (string, bool) {
	if raw == "~" || strings.EqualFold(raw, "null") {
		return "", false
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return unescapeDouble(raw[1 : len(raw)-1]), true
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'"), true
	}
	return raw, false
}

func unescapeDouble(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
