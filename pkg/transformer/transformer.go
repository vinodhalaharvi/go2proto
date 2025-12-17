// Package transformer converts Go types to Protocol Buffer definitions.
package transformer

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/vinodhalaharvi/go2proto/pkg/ct"
	"github.com/vinodhalaharvi/go2proto/pkg/parser"
)

// Proto represents a complete .proto file.
type Proto struct {
	Syntax   string
	Package  string
	Options  map[string]string
	Imports  []string
	Enums    []ProtoEnum
	Messages []ProtoMessage
	Services []ProtoService
}

// ProtoMessage represents a protobuf message.
type ProtoMessage struct {
	Name     string
	Fields   []ProtoField
	Nested   []ProtoMessage
	Enums    []ProtoEnum
	Comments []string
}

// ProtoField represents a field in a message.
type ProtoField struct {
	Name     string
	Type     string
	Number   int
	Repeated bool
	Optional bool
	MapKey   string
	MapValue string
	Comments []string
}

// ProtoEnum represents an enum type.
type ProtoEnum struct {
	Name     string
	Values   []ProtoEnumValue
	Comments []string
}

// ProtoEnumValue represents an enum value.
type ProtoEnumValue struct {
	Name     string
	Number   int
	Comments []string
}

// ProtoService represents a gRPC service.
type ProtoService struct {
	Name     string
	Methods  []ProtoRPC
	Comments []string
}

// ProtoRPC represents an RPC method.
type ProtoRPC struct {
	Name            string
	InputType       string
	OutputType      string
	ClientStreaming bool
	ServerStreaming bool
	Comments        []string
}

// ProtoMonoid allows composing Proto structures.
var ProtoMonoid = ct.Monoid[Proto]{
	Empty: func() Proto {
		return Proto{Syntax: "proto3", Options: make(map[string]string)}
	},
	Append: func(a, b Proto) Proto {
		opts := make(map[string]string)
		for k, v := range a.Options {
			opts[k] = v
		}
		for k, v := range b.Options {
			opts[k] = v
		}
		return Proto{
			Syntax:   ct.Coalesce(a.Syntax, b.Syntax),
			Package:  ct.Coalesce(a.Package, b.Package),
			Options:  opts,
			Imports:  ct.Unique(append(a.Imports, b.Imports...)),
			Enums:    append(a.Enums, b.Enums...),
			Messages: append(a.Messages, b.Messages...),
			Services: append(a.Services, b.Services...),
		}
	},
}

// TypeMapping represents Go to Proto type mapping.
type TypeMapping struct {
	Proto  string
	Import string
}

var defaultTypeMappings = map[string]TypeMapping{
	"string": {Proto: "string"}, "bool": {Proto: "bool"},
	"int": {Proto: "int64"}, "int8": {Proto: "int32"}, "int16": {Proto: "int32"},
	"int32": {Proto: "int32"}, "int64": {Proto: "int64"},
	"uint": {Proto: "uint64"}, "uint8": {Proto: "uint32"}, "uint16": {Proto: "uint32"},
	"uint32": {Proto: "uint32"}, "uint64": {Proto: "uint64"},
	"float32": {Proto: "float"}, "float64": {Proto: "double"},
	"byte": {Proto: "uint32"}, "rune": {Proto: "int32"}, "[]byte": {Proto: "bytes"},
	"time.Time":     {Proto: "google.protobuf.Timestamp", Import: "google/protobuf/timestamp.proto"},
	"time.Duration": {Proto: "google.protobuf.Duration", Import: "google/protobuf/duration.proto"},
	"error":         {Proto: "string"}, "any": {Proto: "google.protobuf.Any", Import: "google/protobuf/any.proto"},
}

// TransformOptions configures the transformation.
type TransformOptions struct {
	PackageName    string
	GoPackage      string
	TypeMappings   map[string]TypeMapping
	IncludePrivate bool
	ServiceSuffix  string
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() TransformOptions {
	return TransformOptions{TypeMappings: defaultTypeMappings, ServiceSuffix: "Service"}
}

// Transformer converts Go packages to Proto definitions.
type Transformer struct {
	opts       TransformOptions
	knownTypes map[string]bool
}

// NewTransformer creates a new transformer.
func NewTransformer(opts TransformOptions) *Transformer {
	return &Transformer{opts: opts, knownTypes: make(map[string]bool)}
}

// Transform converts Go packages to a Proto definition.
func (t *Transformer) Transform(pkgs []parser.GoPackage) Proto {
	return ct.FoldMap(pkgs, ProtoMonoid, t.transformPackage)
}

func (t *Transformer) transformPackage(pkg parser.GoPackage) Proto {
	protoPackage := t.opts.PackageName
	if protoPackage == "" {
		protoPackage = toProtoPackage(pkg.Path)
	}
	goPackage := t.opts.GoPackage
	if goPackage == "" {
		goPackage = pkg.Path
	}

	enumLookup := t.buildEnumLookup(pkg)

	base := Proto{
		Syntax:  "proto3",
		Package: protoPackage,
		Options: map[string]string{"go_package": goPackage},
	}

	enums := t.transformEnums(pkg, enumLookup)
	messages := ct.FoldMap(pkg.Structs, ProtoMonoid, func(s parser.GoStruct) Proto {
		return t.transformStruct(s, enumLookup)
	})
	services := ct.FoldMap(pkg.Interfaces, ProtoMonoid, func(i parser.GoInterface) Proto {
		return t.transformInterface(i)
	})

	return ct.Concat(ProtoMonoid, []Proto{base, enums, messages, services})
}

func (t *Transformer) buildEnumLookup(pkg parser.GoPackage) map[string]bool {
	lookup := make(map[string]bool)
	for _, cg := range pkg.Consts {
		if len(cg.Values) >= 2 {
			lookup[cg.TypeName] = true
		}
	}
	for _, alias := range pkg.Aliases {
		if alias.Tags["go2proto:enum"] == "true" {
			lookup[alias.Name] = true
		}
	}
	return lookup
}

func (t *Transformer) transformEnums(pkg parser.GoPackage, enumLookup map[string]bool) Proto {
	var enums []ProtoEnum
	for _, cg := range pkg.Consts {
		if !enumLookup[cg.TypeName] {
			continue
		}
		enum := ProtoEnum{Name: cg.TypeName}
		for _, cv := range cg.Values {
			enum.Values = append(enum.Values, ProtoEnumValue{
				Name: toEnumValueName(cg.TypeName, cv.Name), Number: int(cv.Value), Comments: cv.Comments,
			})
		}
		enums = append(enums, enum)
	}
	return Proto{Enums: enums}
}

func (t *Transformer) transformStruct(s parser.GoStruct, enumLookup map[string]bool) Proto {
	if s.Tags["go2proto"] == "false" {
		return ProtoMonoid.Empty()
	}
	if len(s.Name) > 0 && unicode.IsLower(rune(s.Name[0])) {
		return ProtoMonoid.Empty()
	}

	// Build type params lookup
	typeParamsLookup := make(map[string]bool)
	for _, tp := range s.TypeParams {
		typeParamsLookup[tp] = true
	}

	msg := ProtoMessage{Name: s.Name, Comments: filterNonTagComments(s.Comments)}
	var imports []string
	fieldNum := 1

	for _, f := range s.Fields {
		if !f.Exported && !t.opts.IncludePrivate {
			continue
		}
		if f.Embedded {
			continue
		}
		protoField, fieldImports := t.transformField(f, fieldNum, enumLookup, typeParamsLookup)
		if protoField.Name != "" {
			msg.Fields = append(msg.Fields, protoField)
			imports = append(imports, fieldImports...)
			fieldNum++
		}
	}

	return Proto{Messages: []ProtoMessage{msg}, Imports: ct.Unique(imports)}
}

func (t *Transformer) transformField(f parser.GoField, num int, enumLookup map[string]bool, typeParamsLookup map[string]bool) (ProtoField, []string) {
	if tag := parseProtobufTag(f.Tag); tag != nil {
		return *tag, nil
	}

	protoType, imports, repeated, _, mapKey, mapValue := t.transformType(f.Type, enumLookup, typeParamsLookup)

	optional := false
	if _, ok := f.Type.(parser.PointerType); ok {
		if isBasicProtoType(protoType) {
			optional = true
		}
	}

	return ProtoField{
		Name: toSnakeCase(f.Name), Type: protoType, Number: num,
		Repeated: repeated, Optional: optional,
		MapKey: mapKey, MapValue: mapValue, Comments: filterNonTagComments(f.Comments),
	}, imports
}

func (t *Transformer) transformType(goType parser.GoType, enumLookup map[string]bool, typeParamsLookup map[string]bool) (protoType string, imports []string, repeated bool, isMap bool, mapKey string, mapValue string) {
	switch v := goType.(type) {
	case parser.BasicType:
		if mapping, ok := t.opts.TypeMappings[v.Name]; ok {
			protoType = mapping.Proto
			if mapping.Import != "" {
				imports = append(imports, mapping.Import)
			}
			return
		}
		protoType = v.Name
		return
	case parser.PointerType:
		return t.transformType(v.Elem, enumLookup, typeParamsLookup)
	case parser.SliceType:
		if basic, ok := v.Elem.(parser.BasicType); ok && basic.Name == "byte" {
			protoType = "bytes"
			return
		}
		innerType, innerImports, _, _, _, _ := t.transformType(v.Elem, enumLookup, typeParamsLookup)
		protoType = innerType
		imports = innerImports
		repeated = true
		return
	case parser.ArrayType:
		innerType, innerImports, _, _, _, _ := t.transformType(v.Elem, enumLookup, typeParamsLookup)
		protoType = innerType
		imports = innerImports
		repeated = true
		return
	case parser.MapType:
		keyType, keyImports, _, _, _, _ := t.transformType(v.Key, enumLookup, typeParamsLookup)
		valueType, valueImports, _, _, _, _ := t.transformType(v.Value, enumLookup, typeParamsLookup)
		isMap = true
		mapKey = keyType
		mapValue = valueType
		protoType = fmt.Sprintf("map<%s, %s>", keyType, valueType)
		imports = append(keyImports, valueImports...)
		return
	case parser.NamedType:
		fullName := v.String()
		if enumLookup[v.Name] {
			protoType = v.Name
			return
		}
		if mapping, ok := t.opts.TypeMappings[fullName]; ok {
			protoType = mapping.Proto
			if mapping.Import != "" {
				imports = append(imports, mapping.Import)
			}
			return
		}
		if v.Package == "time" {
			if v.Name == "Time" {
				protoType = "google.protobuf.Timestamp"
				imports = append(imports, "google/protobuf/timestamp.proto")
				return
			}
			if v.Name == "Duration" {
				protoType = "google.protobuf.Duration"
				imports = append(imports, "google/protobuf/duration.proto")
				return
			}
		}
		// Check if it's a type parameter from the struct's generic definition
		if typeParamsLookup != nil && typeParamsLookup[v.Name] {
			protoType = "google.protobuf.Any"
			imports = append(imports, "google/protobuf/any.proto")
			return
		}
		protoType = v.Name
		return
	case parser.InterfaceType:
		protoType = "google.protobuf.Any"
		imports = append(imports, "google/protobuf/any.proto")
		return
	default:
		protoType = "google.protobuf.Any"
		imports = append(imports, "google/protobuf/any.proto")
		return
	}
}

func (t *Transformer) transformInterface(i parser.GoInterface) Proto {
	if i.Tags["go2proto:service"] != "true" && i.Tags["go2proto"] != "service" {
		return ProtoMonoid.Empty()
	}

	serviceName := i.Name
	// Keep the full interface name to avoid collision with message types

	service := ProtoService{Name: serviceName, Comments: filterNonTagComments(i.Comments)}
	var messages []ProtoMessage
	var imports []string

	for _, m := range i.Methods {
		rpc, reqMsg, respMsg, methodImports := t.transformMethod(m, serviceName)
		service.Methods = append(service.Methods, rpc)
		if reqMsg != nil {
			messages = append(messages, *reqMsg)
		}
		if respMsg != nil {
			messages = append(messages, *respMsg)
		}
		imports = append(imports, methodImports...)
	}

	return Proto{Services: []ProtoService{service}, Messages: messages, Imports: ct.Unique(imports)}
}

func (t *Transformer) transformMethod(m parser.GoMethod, serviceName string) (ProtoRPC, *ProtoMessage, *ProtoMessage, []string) {
	rpc := ProtoRPC{Name: m.Name}
	var imports []string
	var reqMsg, respMsg *ProtoMessage

	params := ct.Filter(m.Params, func(p parser.GoParam) bool {
		if named, ok := p.Type.(parser.NamedType); ok {
			return !(named.Package == "context" && named.Name == "Context")
		}
		return true
	})

	if len(params) == 1 {
		if named, ok := params[0].Type.(parser.NamedType); ok {
			rpc.InputType = named.Name
		} else {
			reqMsg = t.generateRequestMessage(m.Name, params)
			rpc.InputType = reqMsg.Name
		}
	} else if len(params) > 1 {
		reqMsg = t.generateRequestMessage(m.Name, params)
		rpc.InputType = reqMsg.Name
	} else {
		rpc.InputType = "google.protobuf.Empty"
		imports = append(imports, "google/protobuf/empty.proto")
	}

	results := ct.Filter(m.Results, func(p parser.GoParam) bool {
		if basic, ok := p.Type.(parser.BasicType); ok {
			return basic.Name != "error"
		}
		return true
	})

	if len(results) == 1 {
		resultType := results[0].Type
		if ptr, ok := resultType.(parser.PointerType); ok {
			resultType = ptr.Elem
		}
		if named, ok := resultType.(parser.NamedType); ok {
			rpc.OutputType = named.Name
		} else {
			respMsg = t.generateResponseMessage(m.Name, results)
			rpc.OutputType = respMsg.Name
		}
	} else if len(results) > 1 {
		respMsg = t.generateResponseMessage(m.Name, results)
		rpc.OutputType = respMsg.Name
	} else {
		rpc.OutputType = "google.protobuf.Empty"
		imports = append(imports, "google/protobuf/empty.proto")
	}

	return rpc, reqMsg, respMsg, imports
}

func (t *Transformer) generateRequestMessage(methodName string, params []parser.GoParam) *ProtoMessage {
	msg := &ProtoMessage{Name: methodName + "Request"}
	for i, p := range params {
		protoType, _, repeated, isMap, mapKey, mapValue := t.transformType(p.Type, nil, nil)
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("arg%d", i+1)
		}
		field := ProtoField{Name: toSnakeCase(name), Type: protoType, Number: i + 1, Repeated: repeated}
		if isMap {
			field.MapKey = mapKey
			field.MapValue = mapValue
		}
		msg.Fields = append(msg.Fields, field)
	}
	return msg
}

func (t *Transformer) generateResponseMessage(methodName string, results []parser.GoParam) *ProtoMessage {
	msg := &ProtoMessage{Name: methodName + "Response"}
	for i, r := range results {
		protoType, _, repeated, isMap, mapKey, mapValue := t.transformType(r.Type, nil, nil)
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("result%d", i+1)
		}
		field := ProtoField{Name: toSnakeCase(name), Type: protoType, Number: i + 1, Repeated: repeated}
		if isMap {
			field.MapKey = mapKey
			field.MapValue = mapValue
		}
		msg.Fields = append(msg.Fields, field)
	}
	return msg
}

func toProtoPackage(goPath string) string {
	parts := strings.Split(goPath, "/")
	for i, p := range parts {
		if p == "github.com" || p == "gitlab.com" || p == "bitbucket.org" {
			parts = parts[i+1:]
			break
		}
	}
	result := strings.Join(parts, ".")
	return strings.ReplaceAll(result, "-", "_")
}

func toSnakeCase(s string) string {
	var result strings.Builder
	var prevLower bool
	for i, r := range s {
		isUpper := unicode.IsUpper(r)
		if isUpper {
			if i > 0 {
				nextIsLower := i+1 < len(s) && unicode.IsLower(rune(s[i+1]))
				if prevLower || (nextIsLower && !prevLower && i > 0) {
					result.WriteRune('_')
				}
			}
			result.WriteRune(unicode.ToLower(r))
			prevLower = false
		} else {
			result.WriteRune(r)
			prevLower = true
		}
	}
	return result.String()
}

func toEnumValueName(typeName, valueName string) string {
	upperTypeName := strings.ToUpper(toSnakeCase(typeName))
	upperValueName := strings.ToUpper(toSnakeCase(valueName))
	if strings.HasPrefix(upperValueName, upperTypeName) {
		return upperValueName
	}
	return upperTypeName + "_" + upperValueName
}

func parseProtobufTag(tag string) *ProtoField {
	if tag == "" {
		return nil
	}
	tag = strings.Trim(tag, "`")
	re := regexp.MustCompile(`protobuf:"([^"]+)"`)
	matches := re.FindStringSubmatch(tag)
	if len(matches) < 2 {
		return nil
	}
	parts := strings.Split(matches[1], ",")
	if len(parts) < 3 {
		return nil
	}
	field := &ProtoField{}
	fmt.Sscanf(parts[1], "%d", &field.Number)
	for _, part := range parts[2:] {
		if strings.HasPrefix(part, "name=") {
			field.Name = strings.TrimPrefix(part, "name=")
		}
		if part == "rep" {
			field.Repeated = true
		}
		if part == "opt" {
			field.Optional = true
		}
	}
	switch parts[0] {
	case "bytes":
		field.Type = "bytes"
	case "varint":
		field.Type = "int64"
	case "fixed64":
		field.Type = "fixed64"
	case "fixed32":
		field.Type = "fixed32"
	}
	return field
}

func filterNonTagComments(comments []string) []string {
	return ct.Filter(comments, func(c string) bool {
		return !strings.HasPrefix(strings.TrimSpace(c), "+")
	})
}

func isBasicProtoType(t string) bool {
	switch t {
	case "string", "bool", "bytes", "int32", "int64", "uint32", "uint64",
		"sint32", "sint64", "fixed32", "fixed64", "sfixed32", "sfixed64", "float", "double":
		return true
	}
	return false
}
