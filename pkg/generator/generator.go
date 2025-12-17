// Package generator renders Proto definitions to .proto file format.
package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vinodhalaharvi/go2proto/pkg/ct"
	"github.com/vinodhalaharvi/go2proto/pkg/transformer"
)

// Code represents generated code.
type Code struct {
	Lines []string
}

// CodeMonoid composes code blocks.
var CodeMonoid = ct.Monoid[Code]{
	Empty:  func() Code { return Code{} },
	Append: func(a, b Code) Code { return Code{Lines: append(a.Lines, b.Lines...)} },
}

// Line creates a single line.
func Line(s string) Code { return Code{Lines: []string{s}} }

// Blank creates an empty line.
func Blank() Code { return Line("") }

// Indent adds indentation.
func Indent(c Code) Code {
	indented := make([]string, len(c.Lines))
	for i, line := range c.Lines {
		if line != "" {
			indented[i] = "  " + line
		}
	}
	return Code{Lines: indented}
}

// Comment creates a comment line.
func Comment(s string) Code {
	if s == "" {
		return CodeMonoid.Empty()
	}
	return Line("// " + s)
}

// String converts Code to string.
func (c Code) String() string { return strings.Join(c.Lines, "\n") }

// Generator renders Proto to .proto format.
type Generator struct{}

// NewGenerator creates a new generator.
func NewGenerator() *Generator { return &Generator{} }

// Generate renders a Proto to .proto content.
func (g *Generator) Generate(p transformer.Proto) string {
	code := ct.Concat(CodeMonoid, []Code{
		g.renderHeader(p),
		Blank(),
		g.renderPackage(p),
		Blank(),
		g.renderOptions(p),
		g.renderImports(p),
		g.renderEnums(p),
		g.renderMessages(p),
		g.renderServices(p),
	})
	return code.String()
}

func (g *Generator) renderHeader(p transformer.Proto) Code {
	syntax := p.Syntax
	if syntax == "" {
		syntax = "proto3"
	}
	return Line(fmt.Sprintf(`syntax = "%s";`, syntax))
}

func (g *Generator) renderPackage(p transformer.Proto) Code {
	if p.Package == "" {
		return CodeMonoid.Empty()
	}
	return Line(fmt.Sprintf(`package %s;`, p.Package))
}

func (g *Generator) renderOptions(p transformer.Proto) Code {
	if len(p.Options) == 0 {
		return CodeMonoid.Empty()
	}
	keys := make([]string, 0, len(p.Options))
	for k := range p.Options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := ct.FoldMap(keys, CodeMonoid, func(k string) Code {
		return Line(fmt.Sprintf(`option %s = "%s";`, k, p.Options[k]))
	})
	return ct.Concat(CodeMonoid, []Code{lines, Blank()})
}

func (g *Generator) renderImports(p transformer.Proto) Code {
	if len(p.Imports) == 0 {
		return CodeMonoid.Empty()
	}
	imports := make([]string, len(p.Imports))
	copy(imports, p.Imports)
	sort.Strings(imports)
	lines := ct.FoldMap(imports, CodeMonoid, func(imp string) Code {
		return Line(fmt.Sprintf(`import "%s";`, imp))
	})
	return ct.Concat(CodeMonoid, []Code{lines, Blank()})
}

func (g *Generator) renderEnums(p transformer.Proto) Code {
	if len(p.Enums) == 0 {
		return CodeMonoid.Empty()
	}
	return ct.FoldMap(p.Enums, CodeMonoid, g.renderEnum)
}

func (g *Generator) renderEnum(e transformer.ProtoEnum) Code {
	comments := ct.FoldMap(e.Comments, CodeMonoid, Comment)
	values := ct.FoldMap(e.Values, CodeMonoid, func(v transformer.ProtoEnumValue) Code {
		valueComments := ct.FoldMap(v.Comments, CodeMonoid, func(c string) Code {
			return Line("  // " + c)
		})
		valueLine := Line(fmt.Sprintf("  %s = %d;", v.Name, v.Number))
		return ct.Concat(CodeMonoid, []Code{valueComments, valueLine})
	})
	return ct.Concat(CodeMonoid, []Code{
		comments, Line(fmt.Sprintf("enum %s {", e.Name)), values, Line("}"), Blank(),
	})
}

func (g *Generator) renderMessages(p transformer.Proto) Code {
	if len(p.Messages) == 0 {
		return CodeMonoid.Empty()
	}
	return ct.FoldMap(p.Messages, CodeMonoid, g.renderMessage)
}

func (g *Generator) renderMessage(m transformer.ProtoMessage) Code {
	comments := ct.FoldMap(m.Comments, CodeMonoid, Comment)
	fields := ct.FoldMap(m.Fields, CodeMonoid, g.renderField)
	nestedEnums := ct.FoldMap(m.Enums, CodeMonoid, func(e transformer.ProtoEnum) Code {
		return Indent(g.renderEnum(e))
	})
	nestedMessages := ct.FoldMap(m.Nested, CodeMonoid, func(nested transformer.ProtoMessage) Code {
		return Indent(g.renderMessage(nested))
	})
	return ct.Concat(CodeMonoid, []Code{
		comments, Line(fmt.Sprintf("message %s {", m.Name)),
		nestedEnums, nestedMessages, fields, Line("}"), Blank(),
	})
}

func (g *Generator) renderField(f transformer.ProtoField) Code {
	comments := ct.FoldMap(f.Comments, CodeMonoid, func(c string) Code {
		return Line("  // " + c)
	})
	var fieldLine string
	if f.MapKey != "" && f.MapValue != "" {
		fieldLine = fmt.Sprintf("  map<%s, %s> %s = %d;", f.MapKey, f.MapValue, f.Name, f.Number)
	} else {
		prefix := ""
		if f.Repeated {
			prefix = "repeated "
		} else if f.Optional {
			prefix = "optional "
		}
		fieldLine = fmt.Sprintf("  %s%s %s = %d;", prefix, f.Type, f.Name, f.Number)
	}
	return ct.Concat(CodeMonoid, []Code{comments, Line(fieldLine)})
}

func (g *Generator) renderServices(p transformer.Proto) Code {
	if len(p.Services) == 0 {
		return CodeMonoid.Empty()
	}
	return ct.FoldMap(p.Services, CodeMonoid, g.renderService)
}

func (g *Generator) renderService(s transformer.ProtoService) Code {
	comments := ct.FoldMap(s.Comments, CodeMonoid, Comment)
	methods := ct.FoldMap(s.Methods, CodeMonoid, g.renderRPC)
	return ct.Concat(CodeMonoid, []Code{
		comments, Line(fmt.Sprintf("service %s {", s.Name)), methods, Line("}"), Blank(),
	})
}

func (g *Generator) renderRPC(r transformer.ProtoRPC) Code {
	comments := ct.FoldMap(r.Comments, CodeMonoid, func(c string) Code {
		return Line("  // " + c)
	})
	inputType := r.InputType
	if r.ClientStreaming {
		inputType = "stream " + inputType
	}
	outputType := r.OutputType
	if r.ServerStreaming {
		outputType = "stream " + outputType
	}
	rpcLine := fmt.Sprintf("  rpc %s(%s) returns (%s);", r.Name, inputType, outputType)
	return ct.Concat(CodeMonoid, []Code{comments, Line(rpcLine)})
}
