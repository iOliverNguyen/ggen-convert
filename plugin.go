package ggen_convert

import (
	"github.com/olvrng/ggen"
	"github.com/olvrng/ggen-convert/plugin"
)

func New() ggen.Plugin {
	return plugin.New()
}
