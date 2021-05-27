package plugin

import (
	"fmt"
	"go/types"
	"io"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	"golang.org/x/tools/go/packages"

	"github.com/olvrng/ggen"
	"github.com/olvrng/ggen/ggutil"
	"github.com/olvrng/ggen/lg"
)

var ll = lg.New()

const Command = "gen:convert"
const ModeType = "convert:type"
const ModeCreate = "convert:create"
const ModeUpdate = "convert:update"

func New() ggen.Plugin {
	return &Convert{
		Qualifier: ggutil.Qualifier{},
	}
}

type Convert struct {
	ggen.Qualifier
}

func (p *Convert) Name() string { return "convert" }

func (p *Convert) Filter(ng ggen.FilterEngine) error {
	// currentInfo = parse.NewInfo(ng)
	for _, pkg := range ng.ParsingPackages() {
		if !ggen.FilterByCommand(Command).Include(pkg.Directives) {
			continue
		}
		pkg.Include()
		apiPkgs, toPkgs, err := parseDirectives(pkg.Directives)
		if err != nil {
			return ggen.Errorf(err, "parsing %v: %v", pkg.PkgPath, err)
		}
		ng.ParsePackages(apiPkgs...)
		ng.ParsePackages(toPkgs...)
	}
	return nil
}

func (p *Convert) Generate(ng ggen.Engine) error {
	// currentInfo.Init(ng)

	var generatingPackages []*generatingPackage
	pkgs := ng.GeneratingPackages()
	for _, pkg := range pkgs {
		p, err := preparePackage(ng, pkg)
		if err != nil {
			return err
		}
		generatingPackages = append(generatingPackages, p)
	}

	pkgPairs := make(map[pkgPairDecl]*generatingPackage)
	for _, gpkg := range generatingPackages {
		for _, step := range gpkg.steps {
			for _, argPkg := range step.argPkgs {
				for _, outPkg := range step.outPkgs {
					pair0 := pkgPairDecl{argPkg.PkgPath, outPkg.PkgPath}
					pair1 := pkgPairDecl{outPkg.PkgPath, argPkg.PkgPath}
					if pkgPairs[pair0] != nil {
						return ggen.Errorf(nil, "multiple packages with same conversion %v->%v (%v and %v)",
							argPkg.PkgPath, outPkg.PkgPath, pkgPairs[pair0].gpkg.PkgPath, gpkg.gpkg.PkgPath)
					}
					pkgPairs[pair0] = gpkg
					pkgPairs[pair1] = gpkg
				}
			}
		}
	}

	convPairs = make(map[convPair]*conversionFunc)
	for _, gpkg := range generatingPackages {
		for _, obj := range gpkg.gpkg.GetObjects() {
			fn, ok := obj.(*types.Func)
			if !ok {
				continue
			}
			sign := fn.Type().(*types.Signature)
			if sign.Recv() != nil {
				continue
			}
			mode, arg, out, err := validateConvertFunc(fn)
			if err != nil {
				ll.V(2).Printf("error in function %v.%v: err", fn.Pkg().Path(), fn.Name(), err)
				return err
			}
			if mode == 0 {
				gpkg.ignoredFuncs = append(gpkg.ignoredFuncs, nameWithComment{
					Name:    fn.Name(),
					Comment: "not recognized",
				})
				ll.V(2).Printf("ignore function %v.%v because it is not a recognized signature format", fn.Pkg().Path(), fn.Name())
				continue
			}
			pkgPair := pkgPairDecl{
				ArgPkg: arg.Pkg().Path(),
				OutPkg: out.Pkg().Path(),
			}
			if gpkg1 := pkgPairs[pkgPair]; gpkg1 != nil && gpkg1 != gpkg {
				return ggen.Errorf(nil,
					"function %v which converts from %v to %v must be defined in %v (found in %v)",
					fn.Name(), arg.Name(), out.Name(),
					gpkg.gpkg.PkgPath, gpkg1.gpkg.PkgPath)
			}
			pair, _, _ := getPairWithPointer(arg, out)
			if !pair.valid {
				gpkg.ignoredFuncs = append(gpkg.ignoredFuncs, nameWithComment{
					Name:    fn.Name(),
					Comment: "params are not pointer to named types",
				})
				ll.V(2).Printf("ignore function %v.%v because its params are not pointer to named types", fn.Pkg().Path(), fn.Name())
				continue
			}
			if convPairs[pair] != nil {
				return ggen.Errorf(nil,
					"duplicated conversion functions from %v to %v (function %v and %v)",
					arg.Type().String(), out.Type().String(), convPairs[pair].Func.Name(), fn.Name())
			}
			customConv := nameWithComment{
				Name:    fn.Name(),
				Comment: "not use, no conversions between params",
			}
			if convInUse(gpkg.objMap, pair) {
				customConv.Comment = "in use"
			}
			gpkg.customConvs = append(gpkg.customConvs, customConv)
			convPairs[pair] = &conversionFunc{
				convPair: pair,
				Func:     fn,
				Mode:     mode,
			}
		}
	}

	for _, gpkg := range generatingPackages {
		gpkg.objList = prepareListObject(gpkg.objMap)
		prepareConverts(convPairs, gpkg.objMap, gpkg.objList)
	}

	for _, gpkg := range generatingPackages {
		currentPrinter = gpkg.gpkg.GetPrinter()
		generateComments(currentPrinter, gpkg.customConvs, gpkg.ignoredFuncs)
		_, err := generateConverts(currentPrinter, gpkg.objMap, gpkg.objList)
		if err != nil {
			return err
		}
	}
	return nil
}

type generatingPackage struct {
	gpkg    *ggen.GeneratingPackage
	objList []objNameDecl
	objMap  map[objNameDecl]*objMapDecl
	steps   []*generatingPackageStep

	customConvs  []nameWithComment
	ignoredFuncs []nameWithComment
}

type generatingPackageStep struct {
	outPkgs []*packages.Package
	argPkgs []*packages.Package
}

type objMapDecl struct {
	src  types.Object
	gens []objGen
}

type objGen struct {
	mode    string
	obj     types.Object
	opts    options
	convPkg *packages.Package
}

type nameWithComment struct {
	Name    string
	Comment string
}

type objNameDecl struct {
	pkg  string
	name string
}

type options struct {
	identifiers []string
}

type fieldConvert struct {
	Arg *types.Var
	Out *types.Var

	IsIdentifier bool
}

type pkgPairDecl struct {
	ArgPkg string
	OutPkg string
}

type convPair struct {
	valid bool
	Arg   objNameDecl
	Out   objNameDecl
}

type conversionFunc struct {
	convPair
	Mode int
	Func *types.Func

	ConverterPkg *packages.Package
}

func (o objNameDecl) String() string {
	if o.pkg == "" {
		return o.name
	}
	return o.pkg + "." + o.name
}

func parseDirectives(ds []ggen.Directive) (apiPkgs, toPkgs []string, _ error) {
	for _, d := range ds {
		if d.Cmd != Command {
			continue
		}
		apiPkgPaths, toPkgPaths, err := parseConvertDirective(d)
		if err != nil {
			return nil, nil, err
		}
		apiPkgs = append(apiPkgs, apiPkgPaths...)
		toPkgs = append(toPkgs, toPkgPaths...)
	}
	return
}

func preparePackage(ng ggen.Engine, gpkg *ggen.GeneratingPackage) (*generatingPackage, error) {
	result := &generatingPackage{
		gpkg:   gpkg,
		objMap: make(map[objNameDecl]*objMapDecl),
	}
	for _, d := range gpkg.GetDirectives() {
		if d.Cmd != Command {
			continue
		}
		apiPkgPaths, toPkgPaths, err := parseConvertDirective(d)
		if err != nil {
			return nil, err
		}
		step, err := generatePackageStep(ng, gpkg, gpkg.GetPrinter(), result.objMap, apiPkgPaths, toPkgPaths)
		if err != nil {
			return nil, err
		}
		result.steps = append(result.steps, step)
	}
	if len(result.steps) == 0 {
		return nil, ggen.Errorf(nil, "convert package %v: invalid directive (must in format pkg1 -> pkg2)", gpkg.PkgPath)
	}
	return result, nil
}

func generatePackageStep(ng ggen.Engine, gpkg *ggen.GeneratingPackage, p ggen.Printer, apiObjMap map[objNameDecl]*objMapDecl, apiPkgPaths, toPkgPaths []string) (*generatingPackageStep, error) {
	ll.V(1).Printf("convert from %v to %v", strings.Join(apiPkgPaths, ","), strings.Join(toPkgPaths, ","))

	var result generatingPackageStep
	flagSelf, err := validateEquality(apiPkgPaths, toPkgPaths)
	if err != nil {
		return nil, err
	}
	flagAuto := !flagSelf && len(apiPkgPaths) == 1 && len(toPkgPaths) == 1

	apiPkgs := make([]*packages.Package, len(apiPkgPaths))
	for i, pkgPath := range apiPkgPaths {
		apiPkgs[i] = ng.GetPackageByPath(pkgPath)
		if apiPkgs[i] == nil {
			return nil, ggen.Errorf(nil, "can not find package %v", pkgPath)
		}
	}
	toPkgs := make([]*packages.Package, len(toPkgPaths))
	for i, pkgPath := range toPkgPaths {
		toPkgs[i] = ng.GetPackageByPath(pkgPath)
		if toPkgs[i] == nil {
			return nil, ggen.Errorf(nil, "can not find package %v", pkgPath)
		}
	}

	for _, pkg := range apiPkgs {
		apiObjs := ng.GetObjectsByPackage(pkg)
		for _, obj := range apiObjs {
			if !obj.Exported() {
				continue
			}
			objName := objNameDecl{pkg: pkg.PkgPath, name: obj.Name()}
			if apiObjMap[objName] == nil {
				apiObjMap[objName] = &objMapDecl{src: obj}
			}
		}
	}

	if ll.Verbosed(3) {
		for objName, objMap := range apiObjMap {
			ll.V(3).Printf("object %v: %v", objName, objMap.src.Type())
		}
	}

	for _, toPkg := range toPkgs {
		toObjs := ng.GetObjectsByPackage(toPkg)
		for _, obj := range toObjs {
			directives := ng.GetDirectives(obj)
			ll.V(2).Printf("convert to object %v with directives %#v", obj.Name(), directives)
			if !obj.Exported() {
				continue
			}
			if s := validateStruct(obj); s == nil {
				continue
			}

			flagConvert := false
			for _, directive := range directives {
				raw, mode, name, opts, err2 := parseWithMode(apiPkgs, directive)
				if err2 != nil {
					return nil, err2
				}

				ll.V(3).Printf("parsed type %v with mode %v", name, mode)
				if mode != "" && apiObjMap[name] == nil {
					return nil, ggen.Errorf(nil, "type %v not found (directive %v)", name, raw)
				}
				if (name == objNameDecl{}) {
					continue
				}

				flagConvert = true
				m := apiObjMap[name]
				if s := validateStruct(m.src); s == nil {
					return nil, ggen.Errorf(nil, "%v is not a struct", m.src.Name())
				}
				m.gens = append(m.gens, objGen{
					mode:    mode,
					obj:     obj,
					opts:    opts,
					convPkg: gpkg.Package,
				})
			}

			if !flagConvert && flagAuto {
				name := objNameDecl{apiPkgs[0].PkgPath, obj.Name()}
				if apiObjMap[name] == nil {
					continue
				}
				mode := ModeType
				m := apiObjMap[name]
				if s := validateStruct(m.src); s == nil {
					return nil, ggen.Errorf(nil, "%v is not a struct", m.src.Name())
				}
				m.gens = append(m.gens, objGen{
					mode:    mode,
					obj:     obj,
					opts:    options{},
					convPkg: gpkg.Package,
				})
			}
		}
	}
	return &result, nil
}

func validateConvertFunc(fn *types.Func) (mode int, arg, out *types.Var, err error) {
	sign := fn.Type().(*types.Signature)
	params, results := sign.Params(), sign.Results()
	switch {
	case params.Len() == 1 && results.Len() == 1:
		arg = params.At(0)
		out = results.At(0)
		return 1, arg, out, nil

	case params.Len() == 2 && results.Len() == 0:
		arg = params.At(0)
		out = params.At(1)
		return 2, arg, out, nil

	case params.Len() == 2 && results.Len() == 1:
		if validatePtrTypeEquality(params.At(1).Type(), results.At(0).Type()) {
			arg = params.At(0)
			out = params.At(1)
			return 3, arg, out, nil
		}
	}

	return 0, nil, nil, nil
}

func validatePtrTypeEquality(t0, t1 types.Type) bool {
	ptr0, ok0 := t0.(*types.Pointer)
	ptr1, ok1 := t1.(*types.Pointer)
	if !ok0 || !ok1 {
		return false
	}
	return ptr0.Elem() == ptr1.Elem()
}

func validateEquality(pkgs1, pkgs2 []string) (bool, error) {
	if len(pkgs1) != len(pkgs2) {
		return false, nil
	}
	count := 0
	flags := make([]bool, len(pkgs1))
	for i, p1 := range pkgs1 {
		for _, p2 := range pkgs2 {
			if p1 == p2 {
				if flags[i] {
					return false, ggen.Errorf(nil, "duplicated package (%v)", p1)
				}
				count++
				flags[i] = true
			}
		}
	}
	return count == len(pkgs1), nil
}

func validateStruct(obj types.Object) *types.Struct {
	s, ok := obj.(*types.TypeName)
	if !ok {
		return nil
	}
	st, _ := s.Type().Underlying().(*types.Struct)
	return st
}

func parseConvertDirective(directive ggen.Directive) (apiPkgs, toPkgs []string, err error) {

	if !strings.Contains(directive.Arg, "->") {
		pkgs := strings.Split(directive.Arg, ",")
		for i := range pkgs {
			pkgs[i] = strings.TrimSpace(pkgs[i])
		}
		return pkgs, pkgs, nil
	}

	parts := strings.Split(directive.Arg, "->")
	if len(parts) != 2 {
		err = ggen.Errorf(nil, "invalid directive (must in format pkg1 -> pkg2)")
		return
	}

	toPkgs = strings.Split(parts[0], ",")
	for i := range toPkgs {
		toPkgs[i] = strings.TrimSpace(toPkgs[i])
		if toPkgs[i] == "" {
			err = ggen.Errorf(nil, "invalid directive (must in format pkg1 -> pkg2)")
			return
		}
	}

	apiPkgs = strings.Split(parts[1], ",")
	for i := range apiPkgs {
		apiPkgs[i] = strings.TrimSpace(apiPkgs[i])
		if apiPkgs[i] == "" {
			err = ggen.Errorf(nil, "invalid directive (must in format pkg1 -> pkg2)")
			return
		}
	}
	return apiPkgs, toPkgs, nil
}

var reName = regexp.MustCompile(`[A-Z][A-z0-9_]*`)

func parseWithMode(apiPkgs []*packages.Package, d ggen.Directive) (raw, mode string, _ objNameDecl, _ options, _ error) {
	switch d.Cmd {
	case ModeType, ModeCreate, ModeUpdate:
		objName, opts, err := parseTypeName(apiPkgs, d.Arg)
		if err == nil && d.Cmd != ModeUpdate {
			if len(opts.identifiers) != 0 {
				err = ggen.Errorf(nil, "invalid extra option (%v)", d.Arg)
			}
		}
		return d.Raw, d.Cmd, objName, opts, err
	}
	return
}

var reTypeName = regexp.MustCompile(`^(.+\.)?([^.(]+)(\([^)]*\))?$`)

func parseTypeName(apiPkgs []*packages.Package, input string) (_ objNameDecl, opts options, err error) {
	parts := reTypeName.FindStringSubmatch(input)
	if len(parts) == 0 {
		err = ggen.Errorf(nil, "invalid convert directive (%v)", input)
		return
	}
	pkgPath, name, extra := parts[1], parts[2], parts[3]
	if pkgPath != "" {
		pkgPath = pkgPath[:len(pkgPath)-1]
	}
	if extra != "" {
		extra = extra[1 : len(extra)-1]
		opts.identifiers = strings.Split(extra, ",")
		for _, ident := range opts.identifiers {
			if !reName.MatchString(ident) {
				err = ggen.Errorf(nil, "invalid field name (%v)", input)
				return
			}
		}
	}

	if pkgPath == "" {
		if !reName.MatchString(name) {
			err = ggen.Errorf(nil, "invalid type name (%v)", input)
			return
		}
		if len(apiPkgs) == 1 {
			return objNameDecl{pkg: apiPkgs[0].PkgPath, name: name}, opts, nil
		}
		err = ggen.Errorf(nil, "must provide path for multiple input packages (%v)", input)
		return
	}

	pkgPaths := make([]string, len(apiPkgs))
	var thePkg *packages.Package
	for i, pkg := range apiPkgs {
		pkgPaths[i] = pkg.PkgPath
		if hasBase(pkg.PkgPath, pkgPath) {
			if thePkg != nil {
				err = ggen.Errorf(nil, "ambiguous path (%v)", pkgPath)
				return
			}
			thePkg = pkg
		}
	}
	if thePkg == nil {
		err = ggen.Errorf(nil, "invalid package path (%v not found in %v)", pkgPath, strings.Join(pkgPaths, ","))
		return
	}
	if !reName.MatchString(name) {
		err = ggen.Errorf(nil, "invalid type name (%v)", name)
		return
	}
	return objNameDecl{pkg: thePkg.PkgPath, name: name}, opts, nil
}

func generateComments(
	p ggen.Printer,
	customConversions, ignoredFuncs []nameWithComment,
) {
	sort.Slice(customConversions, func(i, j int) bool {
		return customConversions[i].Name < customConversions[j].Name
	})
	sort.Slice(ignoredFuncs, func(i, j int) bool {
		return ignoredFuncs[i].Name < ignoredFuncs[j].Name
	})
	tp := tabwriter.NewWriter(p, 16, 4, 0, ' ', 0)
	w(p, "/*\n")
	w(p, "Custom conversions:")
	if len(customConversions) == 0 {
		w(p, " (none)\n")
	} else {
		w(p, "\n")
	}
	for _, c := range customConversions {
		w(tp, "    %v", c.Name)
		if c.Comment != "" {
			w(tp, "\t    // %v", c.Comment)
		}
		w(tp, "\n")
	}
	_ = tp.Flush()
	w(p, "\nIgnored functions:")
	if len(ignoredFuncs) == 0 {
		w(p, " (none)\n")
	} else {
		w(p, "\n")
	}
	for _, c := range ignoredFuncs {
		w(tp, "    %v\t    // %v\n", c.Name, c.Comment)
	}
	_ = tp.Flush()
	w(p, "*/\n")
}

func convInUse(apiObjMap map[objNameDecl]*objMapDecl, pair convPair) bool {
	for _, item := range [][2]objNameDecl{
		{pair.Arg, pair.Out},
		{pair.Out, pair.Arg},
	} {
		m := apiObjMap[item[0]]
		if m == nil {
			continue
		}
		for _, g := range m.gens {
			name := objNameDecl{
				pkg:  g.obj.Pkg().Path(),
				name: g.obj.Name(),
			}
			if name == item[1] {
				return true
			}
		}
	}
	return false
}

func prepareListObject(apiObjMap map[objNameDecl]*objMapDecl) []objNameDecl {
	list := make([]objNameDecl, 0, len(apiObjMap))
	for objName, obj := range apiObjMap {
		if len(obj.gens) != 0 {
			list = append(list, objName)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].pkg < list[j].pkg {
			return true
		}
		if list[i].pkg > list[j].pkg {
			return false
		}
		return list[i].name < list[j].name
	})
	return list
}

func prepareConverts(
	convPairs map[convPair]*conversionFunc,
	apiObjMap map[objNameDecl]*objMapDecl,
	list []objNameDecl,
) {

	for _, objName := range list {
		m := apiObjMap[objName]
		for _, g := range m.gens {
			if g.mode != ModeType {
				continue
			}
			pairs := []convPair{getPair(m.src, g.obj), getPair(g.obj, m.src)}
			for _, pair := range pairs {
				if convPairs[pair] == nil {
					convPairs[pair] = &conversionFunc{convPair: pair}
				}
				convPairs[pair].ConverterPkg = g.convPkg
			}
		}
	}
}

func generateConverts(
	p ggen.Printer,
	apiObjMap map[objNameDecl]*objMapDecl,
	list []objNameDecl,
) (count int, err error) {
	var conversions []map[string]interface{}
	for _, objName := range list {
		m := apiObjMap[objName]
		for _, g := range m.gens {
			{
				arg, out := g.obj, m.src
				conversion := map[string]interface{}{}
				includeBaseConversion(p, conversion, g.mode, arg, out)
				conversions = append(conversions, conversion)
			}
			if g.mode == ModeType {
				arg, out := m.src, g.obj
				conversion := map[string]interface{}{}
				includeBaseConversion(p, conversion, g.mode, arg, out)
				conversions = append(conversions, conversion)
			}
		}
	}
	{
		p.Import("conversion", "github.com/olvrng/ggen-convert/conversion")
		vars := map[string]interface{}{
			"Conversions": conversions,
		}
		if err2 := tplRegister.Execute(p, vars); err2 != nil {
			return 0, err2
		}
	}

	for _, objName := range list {
		m := apiObjMap[objName]
		if len(m.gens) == 0 {
			continue
		}
		w(p, "//-- convert %v.%v --//\n", m.src.Pkg().Path(), m.src.Name())
		for _, g := range m.gens {
			var err2 error
			switch g.mode {
			case ModeType:
				err2 = generateConvertType(p, g.obj, m.src)
			case ModeCreate:
				err2 = generateCreate(p, g.obj, m.src)
			case ModeUpdate:
				err2 = generateUpdate(p, g.obj, m.src, g.opts)
			default:
				panic("unexpected")
			}
			if err2 != nil {
				return count, ggen.Errorf(err2, "can not convert between %v.%v and %v.%v: %v",
					g.obj.Pkg().Path(), g.obj.Name(), g.obj.Pkg().Path(), m.src.Name(), err2)
			}
			count++
		}
	}
	return count, nil
}

func generateConvertType(p ggen.Printer, src, dst types.Object) error {
	if err := generateConvertTypeImpl(p, src, dst); err != nil {
		return err
	}
	return generateConvertTypeImpl(p, dst, src)
}

func generateConvertTypeImpl(p ggen.Printer, in types.Object, out types.Object) error {
	inSt := validateStruct(in)
	outSt := validateStruct(out)
	fields := make([]fieldConvert, 0, outSt.NumFields())
	embeddedArg, embeddedOut := validateEmbedded(in, out)
	for i, n := 0, outSt.NumFields(); i < n; i++ {
		outField := outSt.Field(i)
		inField := matchField(outField, inSt)
		if inField != nil || outField != embeddedOut {
			fields = append(fields, fieldConvert{
				Arg: inField,
				Out: outField,
			})
		}
	}
	if embeddedArg != nil {
		fields = nil
	}
	vars := map[string]interface{}{
		"Fields":      fields,
		"EmbeddedArg": embeddedArg,
		"EmbeddedOut": embeddedOut,
	}
	includeBaseConversion(p, vars, ModeType, in, out)
	includeCustomConversion(p, vars, in, out)
	return tplConvertType.Execute(p, vars)
}

func generateCreate(p ggen.Printer, arg types.Object, out types.Object) error {
	outSt := validateStruct(out)
	argSt := validateStruct(arg)
	fields := make([]fieldConvert, 0, outSt.NumFields())
	for i, n := 0, outSt.NumFields(); i < n; i++ {
		outField := outSt.Field(i)
		argField := matchField(outField, argSt)
		fields = append(fields, fieldConvert{
			Arg: argField,
			Out: outField,
		})
	}
	vars := map[string]interface{}{
		"Fields": fields,
	}
	includeBaseConversion(p, vars, ModeCreate, arg, out)
	includeCustomConversion(p, vars, arg, out)
	return tplCreate.Execute(p, vars)
}

func generateUpdate(p ggen.Printer, arg types.Object, out types.Object, opts options) error {
	outSt := validateStruct(out)
	argSt := validateStruct(arg)
	fields := make([]fieldConvert, 0, outSt.NumFields())
	identCount := 0
	for i, n := 0, outSt.NumFields(); i < n; i++ {
		outField := outSt.Field(i)
		argField := matchField(outField, argSt)
		isIdentifier := contains(opts.identifiers, outField.Name())
		if isIdentifier {
			identCount++
		}
		fields = append(fields, fieldConvert{
			Arg: argField,
			Out: outField,

			IsIdentifier: isIdentifier,
		})
	}
	if identCount != len(opts.identifiers) {
		return fmt.Errorf("update %v: identifier not found (%v)", arg.Name(), strings.Join(opts.identifiers, ","))
	}
	vars := map[string]interface{}{
		"Fields": fields,
	}
	includeBaseConversion(p, vars, ModeUpdate, arg, out)
	includeCustomConversion(p, vars, arg, out)
	return tplUpdate.Execute(p, vars)
}

func includeBaseConversion(p ggen.Printer, vars map[string]interface{}, mode string, arg types.Object, out types.Object) {
	outType := p.TypeString(out.Type())
	argType := p.TypeString(arg.Type())
	vars["ArgStr"] = strings.ReplaceAll(argType, ".", "_")
	vars["OutStr"] = strings.ReplaceAll(outType, ".", "_")
	vars["ArgType"] = argType
	vars["OutType"] = outType

	switch mode {
	case ModeType:
		vars["Actions"] = "Convert"
		vars["action"] = "convert"
	case ModeCreate, ModeUpdate:
		vars["Actions"] = "Apply"
		vars["action"] = "apply"
	default:
		panic("unexpected")
	}
}

func includeCustomConversion(p ggen.Printer, vars map[string]interface{}, arg types.Object, out types.Object) {
	vars["CustomConversionMode"] = 0
	if conv := convPairs[getPair(arg, out)]; conv != nil && conv.Func != nil {
		vars["CustomConversionMode"] = conv.Mode
		funcName := conv.Func.Name()
		alias := p.Qualifier(conv.Func.Pkg())
		if alias != "" {
			funcName = alias + "." + funcName
		}
		vars["CustomConversionFuncName"] = funcName
	}
}

func validateEmbedded(in, out types.Object) (inField, outField *types.Var) {
	inSt := validateStruct(in)
	outSt := validateStruct(out)
	for i, n := 0, inSt.NumFields(); i < n; i++ {
		_inField := inSt.Field(i)
		if _inField.Embedded() && skipPointer(_inField.Type()) == out.Type() {
			return _inField, nil
		}
	}
	for i, n := 0, outSt.NumFields(); i < n; i++ {
		_outField := outSt.Field(i)
		if _outField.Embedded() && skipPointer(_outField.Type()) == in.Type() {
			return nil, _outField
		}
	}
	return nil, nil
}

func skipPointer(typ types.Type) types.Type {
	ptr, ok := typ.(*types.Pointer)
	if ok {
		return ptr.Elem()
	}
	return typ
}

func validateCompatible(arg, out types.Object) bool {
	if arg.Type() == out.Type() {
		return true
	}
	{

		ptr0, ok0 := arg.Type().(*types.Pointer)
		ptr1, ok1 := out.Type().(*types.Pointer)
		if ok0 && ok1 && ptr0.Elem() == ptr1.Elem() {
			ll.V(1).Printf("*Type %v %v", arg, out)
			return true
		}
	}
	{

		slice0, ok0 := arg.Type().(*types.Slice)
		slice1, ok1 := out.Type().(*types.Slice)
		if ok0 && ok1 && slice0.Elem() == slice1.Elem() {
			ll.V(1).Printf("[]Type %v %v", arg, out)
			return true
		}

		if ok0 && ok1 {
			ptr0, ptrok0 := slice0.Elem().(*types.Pointer)
			ptr1, ptrok1 := slice1.Elem().(*types.Pointer)
			if ptrok0 && ptrok1 && ptr0.Elem() == ptr1.Elem() {
				ll.V(1).Printf("[]*Type %v %v", arg, out)
				return true
			}
		}
	}
	return false
}

func validateSliceToPointerNamed(obj types.Type) *types.Named {
	if typ, ok := obj.(*types.Named); ok {
		obj = typ.Underlying()
	}
	slice, ok := obj.(*types.Slice)
	if !ok {
		return nil
	}
	ptr, ok := slice.Elem().(*types.Pointer)
	if !ok {
		return nil
	}
	named, _ := ptr.Elem().(*types.Named)
	return named
}

func validatePointerToNamed(obj types.Type) *types.Named {
	typ, ok := obj.(*types.Pointer)
	if !ok {
		return nil
	}
	named, _ := typ.Elem().(*types.Named)
	return named
}

func getPairWithSlice(arg, out types.Object) (result convPair, argNamed, outNamed *types.Named) {
	argNamed = validateSliceToPointerNamed(arg.Type())
	outNamed = validateSliceToPointerNamed(out.Type())
	if argNamed == nil {
		return
	}
	if outNamed == nil {
		return
	}
	result = convPair{
		valid: true,
		Arg:   getObjName(argNamed),
		Out:   getObjName(outNamed),
	}
	return
}

func getPairWithPointer(arg, out types.Object) (result convPair, argNamed, outNamed *types.Named) {
	argNamed = validatePointerToNamed(arg.Type())
	outNamed = validatePointerToNamed(out.Type())
	if argNamed == nil {
		ll.V(3).Printf("ignore type %v because it is not a pointer to a named type", arg.Type())
		return
	}
	if outNamed == nil {
		ll.V(3).Printf("ignore type %v because it is not a pointer to a named type", out.Type())
		return
	}
	result = convPair{
		valid: true,
		Arg:   getObjName(argNamed),
		Out:   getObjName(outNamed),
	}
	return
}

func getPair(arg, out types.Object) convPair {
	argNamed, _ := arg.Type().(*types.Named)
	outNamed, _ := out.Type().(*types.Named)
	if argNamed == nil || outNamed == nil {
		return convPair{}
	}
	return convPair{
		valid: true,
		Arg:   getObjName(argNamed),
		Out:   getObjName(outNamed),
	}
}

func getObjName(named *types.Named) objNameDecl {
	return objNameDecl{
		pkg:  named.Obj().Pkg().Path(),
		name: named.Obj().Name(),
	}
}

func contains(ss []string, item string) bool {
	for _, s := range ss {
		if s == item {
			return true
		}
	}
	return false
}

func matchField(baseField *types.Var, st *types.Struct) *types.Var {
	for i, n := 0, st.NumFields(); i < n; i++ {
		field := st.Field(i)
		if field.Name() == baseField.Name() {
			return field
		}
	}
	return nil
}

func w(w io.Writer, format string, args ...interface{}) {
	_, err := fmt.Fprintf(w, format, args...)
	if err != nil {
		panic(err)
	}
}
