package status

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// SchemaFile is where the published schema of status --json is committed,
// relative to the repository root.
const SchemaFile = "schema/status.schema.json"

// Schema returns the JSON Schema (draft 2020-12) of the status --json
// output, generated from PersonView and RepoView by following what
// encoding/json prints for each Go type. A type it cannot describe
// faithfully is an error naming the type and field.
func Schema() ([]byte, error) {
	g := &schemaGen{defs: map[string]any{}, types: map[string]reflect.Type{}}
	var views []any
	for _, v := range []any{PersonView{}, RepoView{}} {
		s, err := g.schema(reflect.TypeOf(v), false, reflect.TypeOf(v).Name())
		if err != nil {
			return nil, err
		}
		views = append(views, s)
	}
	doc := &object{}
	doc.set("$schema", "https://json-schema.org/draft/2020-12/schema")
	doc.set("title", "gh nibfin status --json")
	doc.set("description", "The object gh nibfin status --json prints, and the result of the Dashboard's status route: "+
		"the person view without -R, the repository view with it. Generated from the Go types; "+
		"regenerate with go test ./internal/status -run TestSchemaFile -update.")
	doc.set("oneOf", views)
	doc.set("$defs", g.defs)
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

var (
	errorType     = reflect.TypeOf(op.Error{})
	marshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textType      = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

type schemaGen struct {
	defs  map[string]any
	types map[string]reflect.Type
}

// object is a JSON object that keeps its keys in insertion order.
type object struct {
	keys []string
	vals map[string]any
}

func (o *object) set(k string, v any) {
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *object) has(k string) bool {
	_, ok := o.vals[k]
	return ok
}

func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func obj(kv ...any) *object {
	o := &object{}
	for i := 0; i+1 < len(kv); i += 2 {
		o.set(kv[i].(string), kv[i+1])
	}
	return o
}

// nilable is whether encoding/json can print a value of t as null.
func nilable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		return true
	}
	return false
}

func typeOf(name string, nullable bool) any {
	if nullable {
		return []string{name, "null"}
	}
	return name
}

func orNull(s any, nullable bool) any {
	if !nullable {
		return s
	}
	return obj("anyOf", []any{s, obj("type", "null")})
}

func marshals(t reflect.Type) bool {
	return t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType) ||
		t.Implements(textType) || reflect.PointerTo(t).Implements(textType)
}

// schema describes a value of t at where (Type.Field…); nullable is
// whether it can be printed as null there.
func (g *schemaGen) schema(t reflect.Type, nullable bool, where string) (any, error) {
	if t == errorType || t == reflect.PointerTo(errorType) {
		g.errorDef()
		return orNull(obj("$ref", "#/$defs/op.Error"), nullable), nil
	}
	if marshals(t) {
		return nil, fmt.Errorf("status schema: %s: type %s has its own JSON or text encoding", where, t)
	}
	switch t.Kind() {
	case reflect.Pointer:
		return g.schema(t.Elem(), nullable, where)
	case reflect.Bool:
		return obj("type", typeOf("boolean", nullable)), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return obj("type", typeOf("integer", nullable)), nil
	case reflect.Float32, reflect.Float64:
		return obj("type", typeOf("number", nullable)), nil
	case reflect.String:
		return obj("type", typeOf("string", nullable)), nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return nil, fmt.Errorf("status schema: %s: type %s prints as base64", where, t)
		}
		items, err := g.schema(t.Elem(), nilable(t.Elem()), where+"[]")
		if err != nil {
			return nil, err
		}
		return obj("type", typeOf("array", nullable), "items", items), nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String || marshals(t.Key()) {
			return nil, fmt.Errorf("status schema: %s: map key type %s is not a plain string", where, t.Key())
		}
		vals, err := g.schema(t.Elem(), nilable(t.Elem()), where+"[]")
		if err != nil {
			return nil, err
		}
		return obj("type", typeOf("object", nullable), "additionalProperties", vals), nil
	case reflect.Struct:
		if t.Name() == "" {
			s, err := g.object(t, where)
			if err != nil {
				return nil, err
			}
			return orNull(s, nullable), nil
		}
		name, err := g.def(t, where)
		if err != nil {
			return nil, err
		}
		return orNull(obj("$ref", "#/$defs/"+name), nullable), nil
	}
	return nil, fmt.Errorf("status schema: %s: type %s is not supported", where, t)
}

func (g *schemaGen) errorDef() {
	if _, ok := g.defs["op.Error"]; ok {
		return
	}
	g.types["op.Error"] = errorType
	g.defs["op.Error"] = obj(
		"type", "object",
		"properties", obj("code", obj("type", "string"), "message", obj("type", "string")),
		"required", []string{"code", "message"},
		"additionalProperties", false,
	)
}

// def adds the named struct type t to $defs as <package>.<Type>.
func (g *schemaGen) def(t reflect.Type, where string) (string, error) {
	name := path.Base(t.PkgPath()) + "." + t.Name()
	if prev, ok := g.types[name]; ok {
		if prev != t {
			return "", fmt.Errorf("status schema: %s: %s and %s share the name %s", where, prev, t, name)
		}
		return name, nil
	}
	g.types[name] = t
	s, err := g.object(t, t.Name())
	if err != nil {
		return "", err
	}
	g.defs[name] = s
	return name, nil
}

// object describes struct t as encoding/json prints it.
func (g *schemaGen) object(t reflect.Type, where string) (*object, error) {
	props := &object{}
	required := []string{}
	if err := g.fields(t, props, &required, where); err != nil {
		return nil, err
	}
	return obj("type", "object", "properties", props, "required", required, "additionalProperties", false), nil
}

func (g *schemaGen) fields(t reflect.Type, props *object, required *[]string, where string) error {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		at := where + "." + f.Name
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			switch {
			case f.Type.Kind() == reflect.Pointer:
				return fmt.Errorf("status schema: %s: embedded pointer %s is not supported", at, f.Type)
			case f.Type.Kind() == reflect.Struct:
				if marshals(f.Type) {
					return fmt.Errorf("status schema: %s: type %s has its own JSON or text encoding", at, f.Type)
				}
				if err := g.fields(f.Type, props, required, where); err != nil {
					return err
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		omit := false
		for _, o := range strings.Split(opts, ",") {
			switch o {
			case "omitempty", "omitzero":
				omit = true
			case "string":
				return fmt.Errorf("status schema: %s: the ,string option is not supported", at)
			}
		}
		if props.has(name) {
			return fmt.Errorf("status schema: %s: JSON name %q is used twice", at, name)
		}
		s, err := g.schema(f.Type, !omit && nilable(f.Type), at)
		if err != nil {
			return err
		}
		props.set(name, s)
		if !omit {
			*required = append(*required, name)
		}
	}
	return nil
}
