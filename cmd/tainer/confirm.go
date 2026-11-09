package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// confirm prints question + " [y/N] " and reports whether the answer was
// y or yes. Anything else, including EOF, is no. Pass a *bufio.Reader
// to ask several questions in a row: it is reused, so buffered answers
// for later prompts aren't lost.
func confirm(in io.Reader, out io.Writer, question string) bool {
	fmt.Fprintf(out, "%s [y/N] ", question)
	br, ok := in.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(in)
	}
	line, _ := br.ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}
