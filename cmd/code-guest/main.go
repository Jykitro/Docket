// This WASI guest is given only stdin and stdout by the worker.
package main

import (
	"os"

	"docket/internal/codeeval"
)

func main() {
	if err := codeeval.Evaluate(os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}
