package yamlconfig

import (
	"io"
	"strings"
	"testing"
)

// tokensFromString drains a Decoder over s and returns every token read
// before the stream ended, along with the terminal error (nil for a clean
// io.EOF).
func tokensFromString(s string) ([]Token, error) {
	dec := NewDecoder(strings.NewReader(s))
	var toks []Token
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return toks, nil
		}
		if err != nil {
			return toks, err
		}
		toks = append(toks, tok)
	}
}

// withoutLines strips the Line field so expected tables don't need to track
// line numbers for every token.
func withoutLines(toks []Token) []Token {
	out := make([]Token, len(toks))
	for i, tok := range toks {
		tok.Line = 0
		out[i] = tok
	}
	return out
}

func TestDecoderTokenStreams(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "simple mapping",
			input: "name: checkout\nreplicas: 3\n",
			want: []Token{
				{Kind: MappingStart},
				{Kind: Key, Value: "name"},
				{Kind: Scalar, Value: "checkout"},
				{Kind: Key, Value: "replicas"},
				{Kind: Scalar, Value: "3"},
				{Kind: MappingEnd},
			},
		},
		{
			name:  "simple sequence",
			input: "- a\n- b\n- c\n",
			want: []Token{
				{Kind: SequenceStart},
				{Kind: Scalar, Value: "a"},
				{Kind: Scalar, Value: "b"},
				{Kind: Scalar, Value: "c"},
				{Kind: SequenceEnd},
			},
		},
		{
			name:  "nested mapping",
			input: "service:\n  name: checkout\n  replicas: 3\n",
			want: []Token{
				{Kind: MappingStart},
				{Kind: Key, Value: "service"},
				{Kind: MappingStart},
				{Kind: Key, Value: "name"},
				{Kind: Scalar, Value: "checkout"},
				{Kind: Key, Value: "replicas"},
				{Kind: Scalar, Value: "3"},
				{Kind: MappingEnd},
				{Kind: MappingEnd},
			},
		},
		{
			name:  "sequence of mappings",
			input: "- name: LOG_LEVEL\n  value: info\n- name: REGION\n",
			want: []Token{
				{Kind: SequenceStart},
				{Kind: MappingStart},
				{Kind: Key, Value: "name"},
				{Kind: Scalar, Value: "LOG_LEVEL"},
				{Kind: Key, Value: "value"},
				{Kind: Scalar, Value: "info"},
				{Kind: MappingEnd},
				{Kind: MappingStart},
				{Kind: Key, Value: "name"},
				{Kind: Scalar, Value: "REGION"},
				{Kind: MappingEnd},
				{Kind: SequenceEnd},
			},
		},
		{
			name:  "null values",
			input: "a:\nb: ~\nc: null\nd: Null\n",
			want: []Token{
				{Kind: MappingStart},
				{Kind: Key, Value: "a"},
				{Kind: Scalar},
				{Kind: Key, Value: "b"},
				{Kind: Scalar},
				{Kind: Key, Value: "c"},
				{Kind: Scalar},
				{Kind: Key, Value: "d"},
				{Kind: Scalar},
				{Kind: MappingEnd},
			},
		},
		{
			name: "quoted scalars",
			input: "single: 'it''s here'\n" +
				`double: "line\nbreak"` + "\n" +
				`empty: ""` + "\n",
			want: []Token{
				{Kind: MappingStart},
				{Kind: Key, Value: "single"},
				{Kind: Scalar, Value: "it's here", Quoted: true},
				{Kind: Key, Value: "double"},
				{Kind: Scalar, Value: "line\nbreak", Quoted: true},
				{Kind: Key, Value: "empty"},
				{Kind: Scalar, Value: "", Quoted: true},
				{Kind: MappingEnd},
			},
		},
		{
			name:  "comments and blank lines are ignored",
			input: "# top comment\nfoo: bar # trailing\n\nbaz: qux\n",
			want: []Token{
				{Kind: MappingStart},
				{Kind: Key, Value: "foo"},
				{Kind: Scalar, Value: "bar"},
				{Kind: Key, Value: "baz"},
				{Kind: Scalar, Value: "qux"},
				{Kind: MappingEnd},
			},
		},
		{
			name:  "document markers are ignored",
			input: "---\nfoo: bar\n...\n",
			want: []Token{
				{Kind: MappingStart},
				{Kind: Key, Value: "foo"},
				{Kind: Scalar, Value: "bar"},
				{Kind: MappingEnd},
			},
		},
		{
			name:  "trailing empty sequence item is null",
			input: "- foo\n-\n",
			want: []Token{
				{Kind: SequenceStart},
				{Kind: Scalar, Value: "foo"},
				{Kind: Scalar},
				{Kind: SequenceEnd},
			},
		},
		{
			name:  "bare root scalar",
			input: "just a string\n",
			want: []Token{
				{Kind: Scalar, Value: "just a string"},
			},
		},
		{
			name:  "missing final newline still closes open containers",
			input: "foo: bar",
			want: []Token{
				{Kind: MappingStart},
				{Kind: Key, Value: "foo"},
				{Kind: Scalar, Value: "bar"},
				{Kind: MappingEnd},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tokensFromString(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got = withoutLines(got)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d tokens, want %d\ngot:  %+v\nwant: %+v", len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("token %d: got %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDecoderErrors(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "tabs in indentation",
			input:   "\tfoo: bar\n",
			wantErr: "yamlconfig: line 1: tabs are not allowed for indentation",
		},
		{
			name:    "unexpected indentation increase",
			input:   "foo: 1\n  bar: 2\n",
			wantErr: "yamlconfig: line 2: unexpected indentation",
		},
		{
			name:    "indented content that is neither key nor sequence item",
			input:   "foo:\n  bar\n",
			wantErr: "yamlconfig: line 2: expected a mapping entry or sequence item",
		},
		{
			name:    "mapping entry without a colon",
			input:   "foo: 1\nbar\n",
			wantErr: `yamlconfig: line 2: expected "key: value"`,
		},
		{
			name:    "sequence expects dash items",
			input:   "- foo\nbar\n",
			wantErr: `yamlconfig: line 2: expected a sequence item ("- ")`,
		},
		{
			name:    "nested inline sequence rejected",
			input:   "- - foo\n",
			wantErr: "yamlconfig: line 1: nested inline sequences are not supported",
		},
		{
			name:    "duplicate key in same mapping",
			input:   "foo: 1\nfoo: 2\n",
			wantErr: `yamlconfig: line 2: duplicate key "foo"`,
		},
		{
			name:    "duplicate key after nested block",
			input:   "foo:\n  a: 1\nfoo: 2\n",
			wantErr: `yamlconfig: line 3: duplicate key "foo"`,
		},
		{
			name:    "second top-level document rejected",
			input:   "foo\nbar\n",
			wantErr: "yamlconfig: line 2: multiple documents are not supported",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tokensFromString(tc.input)
			if err == nil {
				t.Fatalf("expected an error, got none")
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("got error %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestDecoderTokenAfterEOFStaysEOF(t *testing.T) {
	dec := NewDecoder(strings.NewReader("foo: 1\n"))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Fatalf("second call after EOF: got %v, want io.EOF", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Fatalf("third call after EOF: got %v, want io.EOF", err)
	}
}
