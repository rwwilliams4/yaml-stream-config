# yaml-stream-config

A validating YAML decoder and pretty printer for Go, standard library only.

## Why

Go has no YAML support in its standard library, so the usual move is to
vendor `gopkg.in/yaml.v3` or something like it. That's fine for most
programs, but it's a real dependency to pull in just to check that a config
file is well-formed and reformat it consistently, and most implementations
build a full in-memory tree from the entire input before you can inspect any
of it. That's a problem if the file in question is assembled by another tool
(templating, `helm template`, a build step that concatenates fragments) and
you want to validate it as it's produced rather than after it's all landed
on disk.

This library parses block-style YAML a line at a time and hands back a
stream of tokens, the same shape as `encoding/xml.Decoder.Token`. Memory use
is bounded by how deeply the document is nested, not by its size: a decoder
never buffers the whole input, and the printer never buffers the whole
output either.

## What it supports

This is a subset of YAML, not the whole spec. Currently handled:

- nested block mappings (`key: value`) and block sequences (`- item`)
- sequences of mappings (`- name: foo` followed by more indented keys)
- plain, single-quoted, and double-quoted scalars, with `\"`, `\\`, `\n`,
  `\t` escapes in double-quoted strings
- comments (`#`) and blank lines
- null values, written as an empty value, `~`, or `null`
- duplicate-key detection within a single mapping
- tabs in indentation are rejected, since YAML doesn't allow them

Not yet supported, and worth knowing before you reach for this on a real
config: flow style (`{a: 1}`, `[1, 2]`), anchors and aliases, tags, block
scalars (`|` and `>`), and multiple documents in one stream. Feeding any of
those in produces a clear error rather than a silently wrong parse.

## Usage

As a library:

```go
package main

import (
	"io"
	"log"
	"os"

	yamlconfig "github.com/rwwilliams4/yaml-stream-config"
)

func main() {
	f, err := os.Open("config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	dec := yamlconfig.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("invalid config: %v", err)
		}
		if tok.Kind == yamlconfig.Key {
			log.Println("key:", tok.Value)
		}
	}
}
```

As a command, to validate a file and print it back out reformatted:

```
go run ./cmd/yamlfmt config.yaml
```

or from stdin:

```
cat config.yaml | go run ./cmd/yamlfmt
```

A non-zero exit and a message on stderr means the input didn't parse; the
error includes the line number.

## Example

Input:

```yaml
service:
  name: checkout
  replicas: 3
  env:
    - name: LOG_LEVEL
      value: info
    - name: REGION
```

`yamlfmt` output (normalizes spacing, drops comments, keeps structure):

```yaml
service:
  name: checkout
  replicas: 3
  env:
    - name: LOG_LEVEL
      value: info
    - name: REGION
```
