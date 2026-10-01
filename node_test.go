package yamlconfig

import (
	"io"
	"strings"
	"testing"
)

func TestParseTree(t *testing.T) {
	src := "service:\n" +
		"  name: checkout\n" +
		"  env:\n" +
		"    - name: LOG_LEVEL\n" +
		"      value: info\n" +
		"    - name: REGION\n" +
		"  note: \"\"\n" +
		"  owner:\n"

	root, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root.Kind != MappingNode {
		t.Fatalf("root kind: got %v, want MappingNode", root.Kind)
	}

	svc := root.Get("service")
	if svc == nil {
		t.Fatal("service missing")
	}
	if got := svc.Get("name"); got == nil || got.Value != "checkout" {
		t.Fatalf("service.name: got %+v", got)
	}

	env := svc.Get("env")
	if env == nil || env.Kind != SequenceNode || len(env.Children) != 2 {
		t.Fatalf("service.env: got %+v", env)
	}
	if got := env.Index(0).Get("value"); got == nil || got.Value != "info" {
		t.Fatalf("env[0].value: got %+v", got)
	}
	if got := env.Index(1).Get("value"); got != nil {
		t.Fatalf("env[1].value: got %+v, want nil", got)
	}
	if env.Index(2) != nil || env.Index(-1) != nil {
		t.Fatal("out of range Index should return nil")
	}

	note := svc.Get("note")
	if note == nil || note.IsNull() || !note.Quoted {
		t.Fatalf("service.note should be a quoted empty string, got %+v", note)
	}
	owner := svc.Get("owner")
	if owner == nil || !owner.IsNull() {
		t.Fatalf("service.owner should be null, got %+v", owner)
	}

	if want := []string{"name", "env", "note", "owner"}; strings.Join(svc.Keys, ",") != strings.Join(want, ",") {
		t.Fatalf("key order: got %v, want %v", svc.Keys, want)
	}
}

func TestParseLineNumbers(t *testing.T) {
	root, err := Parse(strings.NewReader("a: 1\nb:\n  c: 2\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := root.Get("b").Get("c").Line; got != 3 {
		t.Fatalf("b.c line: got %d, want 3", got)
	}
}

func TestParseRootScalar(t *testing.T) {
	root, err := Parse(strings.NewReader("hello\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root.Kind != ScalarNode || root.Value != "hello" {
		t.Fatalf("got %+v", root)
	}
}

func TestParseEmptyDocument(t *testing.T) {
	if _, err := Parse(strings.NewReader("# nothing here\n")); err != io.EOF {
		t.Fatalf("got %v, want io.EOF", err)
	}
}

func TestParsePropagatesDecoderErrors(t *testing.T) {
	_, err := Parse(strings.NewReader("a: 1\n  b: 2\n"))
	if err == nil || err.Error() != "yamlconfig: line 2: unexpected indentation" {
		t.Fatalf("got %v", err)
	}
}

func TestNodeAccessorsOnWrongKind(t *testing.T) {
	root, err := Parse(strings.NewReader("- a\n- b\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root.Get("a") != nil {
		t.Fatal("Get on a sequence should return nil")
	}
	var missing *Node
	if missing.Get("a") != nil || missing.Index(0) != nil {
		t.Fatal("accessors on a nil node should return nil")
	}
}

func TestDecoderNodeReadsOneValueAtATime(t *testing.T) {
	dec := NewDecoder(strings.NewReader("a: 1\n"))
	if _, err := dec.Node(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := dec.Node(); err != io.EOF {
		t.Fatalf("second Node call: got %v, want io.EOF", err)
	}
}
