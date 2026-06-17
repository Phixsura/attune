package main

import (
	"fmt"
	"os"
)

func main() {
	rendered, err := renderAll()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeAll(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, name := range sortedNames(rendered) {
		fmt.Println(name)
	}
}
