// Package parser extracts Go types from source code.
package parser

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// GoPackage represents a parsed Go package.
type GoPackage struct {
	Path       string
	Name       string
	Structs    []GoStruct
	Interfaces []GoInterface
	Aliases    []GoAlias
	Consts     []GoConstGroup
}

// GoStruct represents a Go struct type.
type GoStruct struct {
	Name       string
	Fields     []GoField
	Comments   []string
	Tags       map[string]string
	TypeParams []string // Generic type parameters (e.g., ["T", "K", "V"])
}

// GoField represents a struct field.
type GoField struct {
	Name     string
	Type     GoType
	Tag      string
	Embedded bool
	Comments []string
	Exported bool
}

// GoType represents a Go type.
type GoType interface {
	goType()
	String() string
}

// Basic Go types.
type (
	BasicType   struct{ Name string }
	PointerType struct{ Elem GoType }
	SliceType   struct{ Elem GoType }
	ArrayType   struct {
		Elem GoType
		Len  int64
	}
	MapType       struct{ Key, Value GoType }
	NamedType     struct{ Package, Name string }
	InterfaceType struct{ Methods []GoMethod }
	StructType    struct{ Fields []GoField }
	ChanType      struct {
		Elem GoType
		Dir  ast.ChanDir
	}
	FuncType struct{ Params, Results []GoParam }
)

func (BasicType) goType()     {}
func (PointerType) goType()   {}
func (SliceType) goType()     {}
func (ArrayType) goType()     {}
func (MapType) goType()       {}
func (NamedType) goType()     {}
func (InterfaceType) goType() {}
func (StructType) goType()    {}
func (ChanType) goType()      {}
func (FuncType) goType()      {}

func (t BasicType) String() string   { return t.Name }
func (t PointerType) String() string { return "*" + t.Elem.String() }
func (t SliceType) String() string   { return "[]" + t.Elem.String() }
func (t ArrayType) String() string   { return "[]" + t.Elem.String() }
func (t MapType) String() string     { return "map[" + t.Key.String() + "]" + t.Value.String() }
func (t NamedType) String() string {
	if t.Package != "" {
		return t.Package + "." + t.Name
	}
	return t.Name
}
func (t InterfaceType) String() string { return "interface{}" }
func (t StructType) String() string    { return "struct{}" }
func (t ChanType) String() string      { return "chan " + t.Elem.String() }
func (t FuncType) String() string      { return "func()" }

// GoInterface represents a Go interface type.
type GoInterface struct {
	Name     string
	Methods  []GoMethod
	Comments []string
	Tags     map[string]string
}

// GoMethod represents a method.
type GoMethod struct {
	Name    string
	Params  []GoParam
	Results []GoParam
}

// GoParam represents a function parameter.
type GoParam struct {
	Name string
	Type GoType
}

// GoAlias represents a type alias.
type GoAlias struct {
	Name       string
	Underlying GoType
	Comments   []string
	Tags       map[string]string
}

// GoConstGroup represents constants for enum detection.
type GoConstGroup struct {
	TypeName string
	Values   []GoConstValue
}

// GoConstValue represents a constant value.
type GoConstValue struct {
	Name     string
	Value    int64
	Comments []string
}

// Parser extracts Go types from packages.
type Parser struct {
	fset *token.FileSet
}

// NewParser creates a new parser.
func NewParser() *Parser {
	return &Parser{fset: token.NewFileSet()}
}

// ParsePackages parses multiple Go packages.
func (p *Parser) ParsePackages(patterns ...string) ([]GoPackage, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedTypesSizes |
			packages.NeedSyntax |
			packages.NeedTypesInfo,
		Fset: p.fset,
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}

	var result []GoPackage
	for _, pkg := range pkgs {
		goPkg := p.extractPackage(pkg)
		result = append(result, goPkg)
	}
	return result, nil
}

func (p *Parser) extractPackage(pkg *packages.Package) GoPackage {
	goPkg := GoPackage{
		Path: pkg.PkgPath,
		Name: pkg.Name,
	}

	constGroups := make(map[string]*GoConstGroup)

	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				switch d.Tok {
				case token.TYPE:
					for _, spec := range d.Specs {
						ts := spec.(*ast.TypeSpec)
						comments := extractComments(d.Doc)
						tags := extractTags(comments)

						if tags["go2proto"] == "false" {
							continue
						}

						switch t := ts.Type.(type) {
						case *ast.StructType:
							goPkg.Structs = append(goPkg.Structs, p.extractStruct(ts.Name.Name, t, comments, tags, pkg, ts.TypeParams))
						case *ast.InterfaceType:
							goPkg.Interfaces = append(goPkg.Interfaces, p.extractInterface(ts.Name.Name, t, comments, tags, pkg))
						case *ast.Ident, *ast.SelectorExpr:
							goPkg.Aliases = append(goPkg.Aliases, GoAlias{
								Name:       ts.Name.Name,
								Underlying: p.extractType(ts.Type, pkg),
								Comments:   comments,
								Tags:       tags,
							})
						}
					}
				case token.CONST:
					p.extractConsts(d, constGroups, pkg)
				}
			}
		}
	}

	for _, cg := range constGroups {
		if len(cg.Values) > 0 {
			goPkg.Consts = append(goPkg.Consts, *cg)
		}
	}
	return goPkg
}

func (p *Parser) extractStruct(name string, st *ast.StructType, comments []string, tags map[string]string, pkg *packages.Package, typeParams *ast.FieldList) GoStruct {
	s := GoStruct{Name: name, Comments: comments, Tags: tags}

	// Extract type parameter names
	if typeParams != nil {
		for _, field := range typeParams.List {
			for _, name := range field.Names {
				s.TypeParams = append(s.TypeParams, name.Name)
			}
		}
	}

	if st.Fields != nil {
		for _, field := range st.Fields.List {
			s.Fields = append(s.Fields, p.extractField(field, pkg)...)
		}
	}
	return s
}

func (p *Parser) extractField(field *ast.Field, pkg *packages.Package) []GoField {
	var fields []GoField
	fieldType := p.extractType(field.Type, pkg)
	comments := extractComments(field.Doc)
	tag := ""
	if field.Tag != nil {
		tag = field.Tag.Value
	}

	if len(field.Names) == 0 {
		fields = append(fields, GoField{
			Name: typeNameFromGoType(fieldType), Type: fieldType, Tag: tag,
			Embedded: true, Comments: comments, Exported: true,
		})
	} else {
		for _, name := range field.Names {
			fields = append(fields, GoField{
				Name: name.Name, Type: fieldType, Tag: tag,
				Embedded: false, Comments: comments, Exported: ast.IsExported(name.Name),
			})
		}
	}
	return fields
}

func (p *Parser) extractInterface(name string, it *ast.InterfaceType, comments []string, tags map[string]string, pkg *packages.Package) GoInterface {
	iface := GoInterface{Name: name, Comments: comments, Tags: tags}
	if it.Methods != nil {
		for _, m := range it.Methods.List {
			if len(m.Names) == 0 {
				continue
			}
			if ft, ok := m.Type.(*ast.FuncType); ok {
				iface.Methods = append(iface.Methods, GoMethod{
					Name:    m.Names[0].Name,
					Params:  p.extractParams(ft.Params, pkg),
					Results: p.extractParams(ft.Results, pkg),
				})
			}
		}
	}
	return iface
}

func (p *Parser) extractParams(fl *ast.FieldList, pkg *packages.Package) []GoParam {
	if fl == nil {
		return nil
	}
	var params []GoParam
	for _, field := range fl.List {
		paramType := p.extractType(field.Type, pkg)
		if len(field.Names) == 0 {
			params = append(params, GoParam{Type: paramType})
		} else {
			for _, name := range field.Names {
				params = append(params, GoParam{Name: name.Name, Type: paramType})
			}
		}
	}
	return params
}

func (p *Parser) extractType(expr ast.Expr, pkg *packages.Package) GoType {
	switch t := expr.(type) {
	case *ast.Ident:
		if isBasicType(t.Name) {
			return BasicType{Name: t.Name}
		}
		return NamedType{Name: t.Name}
	case *ast.SelectorExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			importPath := ident.Name
			if pkg != nil && pkg.TypesInfo != nil {
				if obj, ok := pkg.TypesInfo.Uses[ident]; ok {
					if pkgName, ok := obj.(*types.PkgName); ok {
						importPath = pkgName.Imported().Path()
					}
				}
			}
			return NamedType{Package: importPath, Name: t.Sel.Name}
		}
		return NamedType{Name: t.Sel.Name}
	case *ast.StarExpr:
		return PointerType{Elem: p.extractType(t.X, pkg)}
	case *ast.ArrayType:
		elem := p.extractType(t.Elt, pkg)
		if t.Len == nil {
			return SliceType{Elem: elem}
		}
		return ArrayType{Elem: elem}
	case *ast.MapType:
		return MapType{Key: p.extractType(t.Key, pkg), Value: p.extractType(t.Value, pkg)}
	case *ast.InterfaceType:
		return InterfaceType{}
	case *ast.StructType:
		return StructType{}
	case *ast.ChanType:
		return ChanType{Elem: p.extractType(t.Value, pkg), Dir: t.Dir}
	case *ast.FuncType:
		return FuncType{Params: p.extractParams(t.Params, pkg), Results: p.extractParams(t.Results, pkg)}
	default:
		return BasicType{Name: "any"}
	}
}

func (p *Parser) extractConsts(gd *ast.GenDecl, groups map[string]*GoConstGroup, pkg *packages.Package) {
	var currentType string
	var iotaValue int64 = 0

	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		// Check if type is specified - only reset iota when type CHANGES
		if vs.Type != nil {
			if ident, ok := vs.Type.(*ast.Ident); ok {
				if currentType != ident.Name {
					currentType = ident.Name
					iotaValue = 0
				}
			}
		}

		if currentType == "" {
			continue
		}

		group, ok := groups[currentType]
		if !ok {
			group = &GoConstGroup{TypeName: currentType}
			groups[currentType] = group
		}

		for _, name := range vs.Names {
			if !ast.IsExported(name.Name) {
				iotaValue++
				continue
			}

			group.Values = append(group.Values, GoConstValue{
				Name: name.Name, Value: iotaValue, Comments: extractComments(vs.Doc),
			})
			iotaValue++
		}
	}
}

func extractComments(cg *ast.CommentGroup) []string {
	if cg == nil {
		return nil
	}
	var comments []string
	for _, c := range cg.List {
		text := strings.TrimPrefix(c.Text, "//")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		comments = append(comments, strings.TrimSpace(text))
	}
	return comments
}

func extractTags(comments []string) map[string]string {
	tags := make(map[string]string)
	for _, line := range comments {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "+") {
			line = line[1:]
			parts := strings.SplitN(line, "=", 2)
			key := strings.TrimSpace(parts[0])
			value := "true"
			if len(parts) > 1 {
				value = strings.TrimSpace(parts[1])
			}
			tags[key] = value
		}
	}
	return tags
}

func isBasicType(name string) bool {
	switch name {
	case "bool", "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "complex64", "complex128",
		"byte", "rune", "uintptr", "error", "any":
		return true
	}
	return false
}

func typeNameFromGoType(t GoType) string {
	switch v := t.(type) {
	case NamedType:
		return v.Name
	case PointerType:
		return typeNameFromGoType(v.Elem)
	default:
		return ""
	}
}
