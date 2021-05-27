package plugin

const tplRegisterText = `
func RegisterConversions(s *conversion.Scheme) {
    registerConversions(s)
}

func registerConversions(s *conversion.Scheme) {
{{range .Conversions -}}
    s.Register((*{{.ArgType}})(nil), (*{{.OutType}})(nil), func(arg, out interface{}) error {
        {{.Actions}}_{{.ArgStr}}_{{.OutStr}}(arg.(*{{.ArgType}}), out.(*{{.OutType}}))
        return nil
    })
    {{if .Actions|eq "Convert" -}}
    s.Register(([]*{{.ArgType}})(nil), (*[]*{{.OutType}})(nil), func(arg, out interface{}) error {
        out0 := {{.Actions}}_{{.ArgStr|plural}}_{{.OutStr|plural}}(arg.([]*{{.ArgType}}))
        *out.(*[]*{{.OutType}}) = out0
        return nil
    })
    {{end -}}
{{end -}}
}
`

const tplConvertCustomText = `
func {{.Actions}}_{{.ArgStr}}_{{.OutStr}}(arg *{{.ArgType}}, out *{{.OutType}}) *{{.OutType}} {
  {{- if .CustomConversionMode|eq 1}}
    return {{.CustomConversionFuncName}}(arg)
  {{- else if .CustomConversionMode|eq 2}}
    if arg == nil {
        return nil
    }
    if out == nil {
        out = &{{.OutType}}{}
    }
  {{.CustomConversionFuncName}}(arg, out)
    return out
  {{- else if .CustomConversionMode|eq 3}}
    return {{.CustomConversionFuncName}}(arg, out)
  {{- else}}
    if arg == nil {
        return nil
    }
    if out == nil {
        out = &{{.OutType}}{}
    }
  {{.action}}_{{.ArgStr}}_{{.OutStr}}(arg, out)
    return out
  {{- end}}
}
`

const tplConvertTypeText = tplConvertCustomText + `
func {{.action}}_{{.ArgStr}}_{{.OutStr}}(arg *{{.ArgType}}, out *{{.OutType}}) {
	{{- .|embeddedConvert -}}
	{{- range .Fields}}
		out.{{.|fieldName}} = {{.|fieldValue "arg"}} {{lastComment -}}
	{{end}}
}

func {{.Actions}}_{{.ArgStr|plural}}_{{.OutStr|plural}}(args []*{{.ArgType}})(outs []*{{.OutType}}) {
  if args == nil {
    return nil
  }
  tmps := make([]{{.OutType}}, len(args))
  outs = make([]*{{.OutType}}, len(args))
	for i := range tmps {
		outs[i] = Convert_{{.ArgStr}}_{{.OutStr}}(args[i], &tmps[i])
  }
  return outs
}
`

const tplCreateText = tplConvertCustomText + `
func {{.action}}_{{.ArgStr}}_{{.OutStr}}(arg *{{.ArgType}}, out *{{.OutType}}) {
	{{- range .Fields}}
		out.{{.|fieldName}} = {{.|fieldValue "arg"}} {{lastComment -}}
	{{end}}
}
`

const tplUpdateText = tplConvertCustomText + `
func {{.action}}_{{.ArgStr}}_{{.OutStr}}(arg *{{.ArgType}}, out *{{.OutType}}) {
  {{- range .Fields}}
	out.{{.|fieldName}} = {{.|fieldApply "arg"}} {{lastComment -}}
  {{end}}
}
`
