// Command yamlfmt validates a YAML config and writes it back out
// reformatted, reading from a file argument or stdin.
package main

import (
	"fmt"
	"io"
	"os"

	yamlconfig "github.com/rwwilliams4/yaml-stream-config"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "yamlfmt:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	in := stdin
	if len(args) > 0 {
		f, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}

	dec := yamlconfig.NewDecoder(in)
	enc := yamlconfig.NewEncoder(stdout)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("parse: %w", err)
		}
		if err := enc.Encode(tok); err != nil {
			return fmt.Errorf("print: %w", err)
		}
	}
	return enc.Flush()
}
