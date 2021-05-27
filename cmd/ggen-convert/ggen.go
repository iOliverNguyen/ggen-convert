package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/olvrng/ggen"
	ggen_convert "github.com/olvrng/ggen-convert"
)

func main() {
	Start(ggen_convert.New())
}

func usage() {
	const text = `
Usage: ggen-convert PATTERN ...

Options:
`
	fmt.Print(text[1:])
	flag.PrintDefaults()
}

func Start(plugins ...ggen.Plugin) {
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		usage()
		os.Exit(2)
	}

	cfg := ggen.Config{}
	must(ggen.RegisterPlugin(plugins...))
	must(ggen.Start(cfg, patterns...))
}

func must(err error) {
	if err != nil {
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}
}
