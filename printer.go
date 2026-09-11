package yamlconfig

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type eframe struct {
	kind               containerKind
	column             int
	suppressIndentOnce bool
}

// Encoder writes a stream of Tokens back out as formatted block-style YAML.
// Like Decoder, it only ever holds the stack of open containers, so
// re-printing a document never requires assembling the whole thing in
// memory: each Encode call writes as soon as it has enough information to.
type Encoder struct {
	w              *bufio.Writer
	stack          []*eframe
	havePendingKey bool
}

// NewEncoder returns an Encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriter(w)}
}

// Flush writes any buffered output to the underlying writer.
func (e *Encoder) Flush() error {
	return e.w.Flush()
}

func (e *Encoder) top() *eframe {
	if len(e.stack) == 0 {
		return nil
	}
	return e.stack[len(e.stack)-1]
}

// Encode writes tok. Tokens must be fed in the order a Decoder would
// produce them for a valid document.
func (e *Encoder) Encode(tok Token) error {
	switch tok.Kind {
	case MappingStart, SequenceStart:
		return e.encodeStart(tok)
	case MappingEnd, SequenceEnd:
		if len(e.stack) == 0 {
			return fmt.Errorf("yamlconfig: encoder: unmatched end token")
		}
		e.stack = e.stack[:len(e.stack)-1]
		return nil
	case Key:
		return e.encodeKey(tok)
	case Scalar:
		return e.encodeScalar(tok)
	default:
		return fmt.Errorf("yamlconfig: encoder: unknown token kind %v", tok.Kind)
	}
}

func (e *Encoder) encodeStart(tok Token) error {
	k := kindMapping
	if tok.Kind == SequenceStart {
		k = kindSequence
	}

	if len(e.stack) == 0 {
		e.stack = append(e.stack, &eframe{kind: k, column: 0})
		return nil
	}

	top := e.top()
	switch top.kind {
	case kindMapping:
		if !e.havePendingKey {
			return fmt.Errorf("yamlconfig: encoder: container start without a preceding key")
		}
		e.havePendingKey = false
		if _, err := e.w.WriteString(":\n"); err != nil {
			return err
		}
		e.stack = append(e.stack, &eframe{kind: k, column: top.column + 2})
	case kindSequence:
		if err := e.writeIndent(top); err != nil {
			return err
		}
		if _, err := e.w.WriteString("- "); err != nil {
			return err
		}
		e.stack = append(e.stack, &eframe{kind: k, column: top.column + 2, suppressIndentOnce: true})
	}
	return nil
}

func (e *Encoder) encodeKey(tok Token) error {
	top := e.top()
	if top == nil || top.kind != kindMapping {
		return fmt.Errorf("yamlconfig: encoder: key token outside a mapping")
	}
	if err := e.writeIndent(top); err != nil {
		return err
	}
	if _, err := e.w.WriteString(formatKey(tok.Value)); err != nil {
		return err
	}
	if _, err := e.w.WriteString(":"); err != nil {
		return err
	}
	e.havePendingKey = true
	return nil
}

func (e *Encoder) encodeScalar(tok Token) error {
	val := formatScalar(tok)

	if e.havePendingKey {
		e.havePendingKey = false
		if val == "" {
			_, err := e.w.WriteString("\n")
			return err
		}
		_, err := e.w.WriteString(" " + val + "\n")
		return err
	}

	top := e.top()
	if top == nil {
		if val == "" {
			val = "null"
		}
		_, err := e.w.WriteString(val + "\n")
		return err
	}
	if top.kind != kindSequence {
		return fmt.Errorf("yamlconfig: encoder: unexpected scalar token")
	}
	if err := e.writeIndent(top); err != nil {
		return err
	}
	if val == "" {
		_, err := e.w.WriteString("-\n")
		return err
	}
	_, err := e.w.WriteString("- " + val + "\n")
	return err
}

func (e *Encoder) writeIndent(f *eframe) error {
	if f.suppressIndentOnce {
		f.suppressIndentOnce = false
		return nil
	}
	_, err := e.w.WriteString(strings.Repeat(" ", f.column))
	return err
}

func formatKey(k string) string {
	if k == "" || needsQuoting(k) {
		return quoteDouble(k)
	}
	return k
}

// formatScalar returns the literal text to print for tok, or "" to mean a
// bare/null value. tok.Quoted always forces quoting, so a quoted empty
// string round-trips distinctly from an unquoted (null) one.
func formatScalar(tok Token) string {
	if tok.Quoted {
		return quoteDouble(tok.Value)
	}
	if tok.Value == "" {
		return ""
	}
	if needsQuoting(tok.Value) {
		return quoteDouble(tok.Value)
	}
	return tok.Value
}

func needsQuoting(s string) bool {
	if strings.TrimSpace(s) != s {
		return true
	}
	if strings.ContainsRune(s, '\n') {
		return true
	}
	if strings.HasPrefix(s, "#") || strings.Contains(s, " #") {
		return true
	}
	if s == "-" || strings.HasPrefix(s, "- ") {
		return true
	}
	if s == "---" || s == "..." {
		return true
	}
	if s[0] == '"' || s[0] == '\'' {
		return true
	}
	return indexUnquotedColon(s) >= 0
}

func quoteDouble(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
