package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// confirm prints question + " [y/N] " and reports whether the answer was
// y or yes. Anything else, including EOF, is no.
func confirm(in io.Reader, out io.Writer, question string) bool {
	fmt.Fprintf(out, "%s [y/N] ", question)
	line, _ := bufio.NewReader(in).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}
