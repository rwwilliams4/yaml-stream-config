package yamlconfig

import (
	"fmt"
	"io"
)

// NodeKind identifies the shape of a Node.
type NodeKind int

const (
	MappingNode NodeKind = iota
	SequenceNode
	ScalarNode
)

func (k NodeKind) String() string {
	switch k {
	case MappingNode:
		return "MappingNode"
	case SequenceNode:
		return "SequenceNode"
	case ScalarNode:
		return "ScalarNode"
	default:
		return "Unknown"
	}
}

// Node is one value in a fully built document tree. Unlike the token stream
// it holds the whole value in memory, so use it for small documents where
// random access matters more than bounded memory.
//
// For a mapping, Keys and Children are parallel slices in source order. For
// a sequence only Children is used. For a scalar, Value and Quoted carry the
// same meaning as on Token.
type Node struct {
	Kind     NodeKind
	Value    string
	Quoted   bool
	Line     int
	Keys     []string
	Children []*Node
}

// IsNull reports whether n is an unquoted empty scalar, which is how the
// decoder represents null.
func (n *Node) IsNull() bool {
	return n.Kind == ScalarNode && n.Value == "" && !n.Quoted
}

// Get returns the value stored under key in a mapping node. It returns nil
// if n is not a mapping or has no such key. Keys are unique within a mapping
// because the decoder rejects duplicates.
func (n *Node) Get(key string) *Node {
	if n == nil || n.Kind != MappingNode {
		return nil
	}
	for i, k := range n.Keys {
		if k == key {
			return n.Children[i]
		}
	}
	return nil
}

// Index returns the i'th child of a sequence node, or nil if n is not a
// sequence or i is out of range.
func (n *Node) Index(i int) *Node {
	if n == nil || n.Kind != SequenceNode || i < 0 || i >= len(n.Children) {
		return nil
	}
	return n.Children[i]
}

// Parse reads an entire document from r and returns its root node. An empty
// document yields io.EOF. The whole stream is consumed, so errors anywhere
// in the input are reported even if they come after the root value.
func Parse(r io.Reader) (*Node, error) {
	dec := NewDecoder(r)
	root, err := dec.Node()
	if err != nil {
		return nil, err
	}
	if tok, err := dec.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("yamlconfig: line %d: unexpected %v after document", tok.Line, tok.Kind)
	}
	return root, nil
}

// Node reads the next complete value from the token stream and returns it as
// a tree. It returns io.EOF if the stream has no more tokens.
func (d *Decoder) Node() (*Node, error) {
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	return d.buildNode(tok)
}

func (d *Decoder) buildNode(tok Token) (*Node, error) {
	switch tok.Kind {
	case Scalar:
		return &Node{Kind: ScalarNode, Value: tok.Value, Quoted: tok.Quoted, Line: tok.Line}, nil
	case MappingStart:
		return d.buildMapping(tok)
	case SequenceStart:
		return d.buildSequence(tok)
	default:
		return nil, fmt.Errorf("yamlconfig: line %d: unexpected %v", tok.Line, tok.Kind)
	}
}

func (d *Decoder) buildMapping(start Token) (*Node, error) {
	n := &Node{Kind: MappingNode, Line: start.Line}
	for {
		tok, err := d.nextInside()
		if err != nil {
			return nil, err
		}
		if tok.Kind == MappingEnd {
			return n, nil
		}
		if tok.Kind != Key {
			return nil, fmt.Errorf("yamlconfig: line %d: expected a key, got %v", tok.Line, tok.Kind)
		}
		valTok, err := d.nextInside()
		if err != nil {
			return nil, err
		}
		child, err := d.buildNode(valTok)
		if err != nil {
			return nil, err
		}
		n.Keys = append(n.Keys, tok.Value)
		n.Children = append(n.Children, child)
	}
}

func (d *Decoder) buildSequence(start Token) (*Node, error) {
	n := &Node{Kind: SequenceNode, Line: start.Line}
	for {
		tok, err := d.nextInside()
		if err != nil {
			return nil, err
		}
		if tok.Kind == SequenceEnd {
			return n, nil
		}
		child, err := d.buildNode(tok)
		if err != nil {
			return nil, err
		}
		n.Children = append(n.Children, child)
	}
}

// nextInside reads a token where the stream must not end, turning a bare
// io.EOF into io.ErrUnexpectedEOF so callers can tell a truncated tree from
// an empty document.
func (d *Decoder) nextInside() (Token, error) {
	tok, err := d.Token()
	if err == io.EOF {
		return Token{}, io.ErrUnexpectedEOF
	}
	return tok, err
}
