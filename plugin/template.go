package plugin

import (
	"fmt"
	"go/types"
	"strings"
	"text/template"

	"github.com/olvrng/ggen"
)

var tplRegister, tplConvertType, tplUpdate, tplCreate *template.Template

// var currentInfo *parse.Info
var currentPrinter ggen.Printer
var convPairs map[convPair]*conversionFunc

func init() {
	funcMap := map[string]interface{}{
		"embeddedConvert": renderEmbeddedConvert,
		"fieldName":       renderFieldName,
		"fieldValue":      renderFieldValue,
		"fieldApply":      renderFieldApply,
		"lastComment":     renderLastComment,
		"plural":          plural,
	}
	parse := func(name, text string) *template.Template {
		return template.Must(template.New(name).Funcs(funcMap).Parse(text))
	}

	tplRegister = parse("register", tplRegisterText)
	tplConvertType = parse("convert_type", tplConvertTypeText)
	tplCreate = parse("create", tplCreateText)
	tplUpdate = parse("update", tplUpdateText)
}

var lastComment string

func renderLastComment() string {
	return lastComment
}

func renderFieldName(field fieldConvert) string {
	return field.Out.Name()
}

func renderFieldValue(prefix string, field fieldConvert) string {
	in, out := field.Arg, field.Out
	if in == nil {
		lastComment = "// no change"
		return "out." + out.Name()
		// return renderZero(out.Type())
	}
	if validateCompatible(in, out) {
		lastComment = "// simple assign"
		return prefix + "." + in.Name()
	}
	if result := renderCustomConversion(in, out, prefix); result != "" {
		return result
	}
	if result := renderSimpleConversion(in, out, prefix); result != "" {
		return result
	}
	lastComment = "// types do not match"
	return "out." + out.Name()
	// return renderZero(out.Type())
}

func renderFieldApply(prefix string, field fieldConvert) string {
	arg, out := field.Arg, field.Out
	if field.IsIdentifier {
		lastComment = "// identifier"
		return "out." + out.Name()
	}
	if arg == nil {
		lastComment = "// no change"
		return "out." + out.Name()
	}
	if validateCompatible(arg, out) {
		lastComment = "// simple assign"
		return "arg." + out.Name()
	}
	// render NullString, NullInt, ...Apply()
	if argType, ok := arg.Type().(*types.Named); ok {
		if checkApplicable(argType) {
			lastComment = "// apply change"
			return prefix + "." + arg.Name() + ".Apply(out." + out.Name() + ")"
		}
	}
	if result := renderCustomConversion(arg, out, prefix); result != "" {
		return result
	}
	if result := renderSimpleConversion(arg, out, prefix); result != "" {
		return result
	}
	lastComment = "// types do not match"
	return "out." + out.Name()
}

func renderCustomConversion(in, out *types.Var, prefix string) string {
	{
		pair, argNamed, outNamed := getPairWithSlice(in, out)
		if pair.valid {
			conv := convPairs[pair]
			if conv == nil {
				return ""
			}
			lastComment = ""
			return renderCustomConversion0(true, argNamed, outNamed, conv, prefix+"."+in.Name())
		}
	}
	{
		pair, argNamed, outNamed := getPairWithPointer(in, out)
		if pair.valid {
			conv := convPairs[pair]
			if conv == nil {
				return ""
			}
			lastComment = ""
			return renderCustomConversion0(false, argNamed, outNamed, conv, prefix+"."+in.Name())
		}
	}
	return ""
}

func renderCustomConversion0(isPlural bool, in, out *types.Named, conv *conversionFunc, inField string) string {
	p := currentPrinter
	inType := p.TypeString(in)
	outType := p.TypeString(out)
	inStr := strings.ReplaceAll(inType, ".", "_")
	outStr := strings.ReplaceAll(outType, ".", "_")
	if isPlural {
		inStr = plural(inStr)
		outStr = plural(outStr)
	}
	var result string
	if isPlural {
		result = "Convert_" + inStr + "_" + outStr + "(" + inField + ")"
	} else {
		result = "Convert_" + inStr + "_" + outStr + "(" + inField + ", nil)"
	}
	if conv.ConverterPkg == nil {
		panic(fmt.Sprintf(
			"There is custom conversion function %v.%v to convert between %v.%v and %v.%v, but no generated conversion package between %v and %v. You must create one (+gen:convert: %v->%v) or delete the custom conversion function.",
			conv.Func.Pkg().Path(), conv.Func.Name(),
			in.Obj().Pkg().Path(), in.Obj().Name(),
			out.Obj().Pkg().Path(), out.Obj().Name(),
			in.Obj().Pkg().Path(), out.Obj().Pkg().Path(),
			out.Obj().Pkg().Path(), in.Obj().Pkg().Path(),
		))
	}
	alias := p.Qualifier(conv.ConverterPkg.Types)
	if alias != "" {
		return alias + "." + result
	}
	return result
}

func renderSimpleConversion(in, out *types.Var, prefix string) string {
	// convert basic types
	inBasic := checkBasicType(in.Type())
	outBasic := checkBasicType(out.Type())
	if inBasic != nil && outBasic != nil {
		outStr := currentPrinter.TypeString(out.Type())
		if inBasic.Kind() == outBasic.Kind() {
			lastComment = "// simple conversion"
			return outStr + "(" + prefix + "." + in.Name() + ")"
		}
		if inBasic.Info()&types.IsNumeric > 0 && outBasic.Info()&types.IsNumeric > 0 {
			lastComment = "// simple conversion"
			return outStr + "(" + prefix + "." + in.Name() + ")"
		}
	}
	return ""
}

func renderZero(typ types.Type) string {
	t := typ
	for ok := true; ok; _, ok = t.(*types.Named) {
		t = t.Underlying()
	}
	switch t := t.(type) {
	case *types.Basic:
		info := t.Info()
		switch {
		case info&types.IsBoolean > 0:
			return "false"
		case info&types.IsNumeric > 0:
			return "0"
		case info&types.IsString > 0:
			return `""`
		default:
			return "0"
		}

	case *types.Struct:
		if t == typ {
			if t.NumFields() == 0 {
				return "struct{}{}"
			}
			panic(fmt.Sprintf("struct must have a name (%v)", t))
		}
		return currentPrinter.TypeString(typ) + "{}"

	default:
		return "nil"
	}
}

func checkBasicType(typ types.Type) *types.Basic {
	for ok := true; ok; _, ok = typ.(*types.Named) {
		typ = typ.Underlying()
	}
	basic, _ := typ.(*types.Basic)
	return basic
}

var cacheApplicable = map[*types.Named]bool{}

func checkApplicable(named *types.Named) bool {
	if applicable, ok := cacheApplicable[named]; ok {
		return applicable
	}
	if !hasPrefixCamel(named.Obj().Name(), "Null") {
		cacheApplicable[named] = false
		return false
	}
	for i, n := 0, named.NumMethods(); i < n; i++ {
		if named.Method(i).Name() == "Apply" {
			cacheApplicable[named] = true
			return true
		}
	}
	// cacheApplicable[named] = currentInfo.IsNullStruct(named, "")
	return cacheApplicable[named]
}

func renderEmbeddedConvert(vars map[string]interface{}) string {
	arg, _ := vars["EmbeddedArg"].(*types.Var)
	out, _ := vars["EmbeddedOut"].(*types.Var)
	switch {
	case arg == nil && out == nil:
		return ""

	case arg != nil && out == nil:
		if _, ok := arg.Type().(*types.Pointer); ok {
			return fmt.Sprintf("*out = *arg.%v // embedded struct", arg.Name())
		}
		return fmt.Sprintf("*out = arg.%v // embedded struct", arg.Name())

	case arg == nil && out != nil:
		if ptr, ok := out.Type().(*types.Pointer); ok {
			return fmt.Sprintf(`
	out.%v = new(%v) // embedded struct
	*out.%v = *arg // embedded struct`,
				out.Name(), currentPrinter.TypeString(ptr.Elem()), out.Name(),
			)[1:]
		}
		return fmt.Sprintf("out.%v = *arg // embedded struct", out.Name())

	default:
		panic("unexpected")
	}
}
