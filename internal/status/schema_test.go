package status

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/op"
)

var update = flag.Bool("update", false, "write "+SchemaFile+" from Schema()")

// schemaPath is SchemaFile from this package's directory.
var schemaPath = filepath.Join("..", "..", filepath.FromSlash(SchemaFile))

const regenerate = "go test ./internal/status -run TestSchemaFile -update"

// TestSchemaFile fails when the committed schema differs from what the Go
// types generate; with -update it writes the file.
func TestSchemaFile(t *testing.T) {
	want, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.MkdirAll(filepath.Dir(schemaPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(schemaPath, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v; run %s", SchemaFile, err, regenerate)
	}
	// A Windows checkout can turn the file's line endings into CRLF.
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale against the Go types; run %s", SchemaFile, regenerate)
	}
}

type sInner struct {
	A string `json:"a"`
}

type sEmbedded struct {
	E      int `json:"e"`
	hidden int
}

type sCases struct {
	sEmbedded
	Req    string         `json:"req"`
	Opt    string         `json:"opt,omitempty"`
	Zero   sInner         `json:"zero,omitzero"`
	Ptr    *string        `json:"ptr"`
	PtrOpt *bool          `json:"ptrOpt,omitempty"`
	Struct *sInner        `json:"struct"`
	List   []sInner       `json:"list"`
	Map    map[string]int `json:"map"`
	Ptrs   []*sInner      `json:"ptrs,omitempty"`
	Err    *op.Error      `json:"err,omitempty"`
	ErrReq *op.Error      `json:"errReq"`
	Skip   string         `json:"-"`
	Dash   string         `json:"-,"`
	NoTag  float64
	Nested map[string]sInner `json:"nested,omitempty"`
	priv   string
}

func schemaOf(t *testing.T, v any) map[string]any {
	t.Helper()
	g := &schemaGen{defs: map[string]any{}, types: map[string]reflect.Type{}}
	s, err := g.schema(reflect.TypeOf(v), false, "x")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]any{"root": s, "defs": g.defs})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSchemaMapping(t *testing.T) {
	out := schemaOf(t, sCases{})
	if jsonOf(t, out["root"]) != `{"$ref":"#/$defs/status.sCases"}` {
		t.Errorf("root = %v", out["root"])
	}
	defs := out["defs"].(map[string]any)
	c := defs["status.sCases"].(map[string]any)
	if c["additionalProperties"] != false || c["type"] != "object" {
		t.Errorf("def = %v", c)
	}
	if got := jsonOf(t, c["required"]); got != `["e","req","ptr","struct","list","map","errReq","-","NoTag"]` {
		t.Errorf("required = %s", got)
	}
	props := c["properties"].(map[string]any)
	for k, want := range map[string]string{
		"e":      `{"type":"integer"}`,
		"req":    `{"type":"string"}`,
		"opt":    `{"type":"string"}`,
		"zero":   `{"$ref":"#/$defs/status.sInner"}`,
		"ptr":    `{"type":["string","null"]}`,
		"ptrOpt": `{"type":"boolean"}`,
		"struct": `{"anyOf":[{"$ref":"#/$defs/status.sInner"},{"type":"null"}]}`,
		"list":   `{"items":{"$ref":"#/$defs/status.sInner"},"type":["array","null"]}`,
		"map":    `{"additionalProperties":{"type":"integer"},"type":["object","null"]}`,
		"ptrs":   `{"items":{"anyOf":[{"$ref":"#/$defs/status.sInner"},{"type":"null"}]},"type":"array"}`,
		"err":    `{"$ref":"#/$defs/op.Error"}`,
		"errReq": `{"anyOf":[{"$ref":"#/$defs/op.Error"},{"type":"null"}]}`,
		"-":      `{"type":"string"}`,
		"NoTag":  `{"type":"number"}`,
		"nested": `{"additionalProperties":{"$ref":"#/$defs/status.sInner"},"type":"object"}`,
	} {
		if got := jsonOf(t, props[k]); got != want {
			t.Errorf("%s = %s, want %s", k, got, want)
		}
	}
	for _, k := range []string{"Skip", "hidden", "priv", "sEmbedded"} {
		if _, ok := props[k]; ok {
			t.Errorf("%s is described", k)
		}
	}
	if got := jsonOf(t, defs["op.Error"]); got != `{"additionalProperties":false,"properties":{"code":{"type":"string"},"message":{"type":"string"}},"required":["code","message"],"type":"object"}` {
		t.Errorf("op.Error = %s", got)
	}
}

type sMarshaler struct{}

func (sMarshaler) MarshalJSON() ([]byte, error) { return []byte(`1`), nil }

type sCollideInner struct {
	A string `json:"a"`
}

func TestSchemaUnsupported(t *testing.T) {
	for name, tc := range map[string]struct {
		v    any
		want string
	}{
		"marshaler": {struct{ M sMarshaler }{}, "x.M: type status.sMarshaler has its own JSON"},
		"time":      {struct{ T time.Time }{}, "x.T: type time.Time has its own JSON"},
		"interface": {struct{ I any }{}, "x.I: type interface {} is not supported"},
		"map key":   {struct{ M map[int]string }{}, "x.M: map key type int"},
		"bytes":     {struct{ B []byte }{}, "x.B: type []uint8 prints as base64"},
		"string opt": {struct {
			N int `json:"n,string"`
		}{}, "x.N: the ,string option"},
		"chan":      {struct{ C chan int }{}, "x.C: type chan int is not supported"},
		"embed ptr": {struct{ *sInner }{}, "x.sInner: embedded pointer"},
		"collision": {struct {
			sCollideInner
			A int `json:"a"`
		}{}, `x.A: JSON name "a" is used twice`},
		"complex": {struct{ Z complex128 }{}, "x.Z: type complex128 is not supported"},
	} {
		g := &schemaGen{defs: map[string]any{}, types: map[string]reflect.Type{}}
		_, err := g.schema(reflect.TypeOf(tc.v), false, "x")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestSchemaDocument(t *testing.T) {
	a, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Schema()
	if !bytes.Equal(a, b) || !bytes.HasSuffix(a, []byte("}\n")) {
		t.Error("schema is not deterministic or lacks its trailing newline")
	}
	var doc map[string]any
	if err := json.Unmarshal(a, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", doc["$schema"])
	}
	if got := jsonOf(t, doc["oneOf"]); got != `[{"$ref":"#/$defs/status.PersonView"},{"$ref":"#/$defs/status.RepoView"}]` {
		t.Errorf("oneOf = %s", got)
	}
	defs := doc["$defs"].(map[string]any)
	for _, k := range []string{"status.Issue", "status.Blocker", "status.Run", "status.NibfinRepo", "op.HostReport", "op.RunnerState", "op.Error"} {
		if defs[k] == nil {
			t.Errorf("no %s", k)
		}
	}
	// The embedded runners.Runner is promoted; its json:"-" fields are not.
	rs := defs["op.RunnerState"].(map[string]any)["properties"].(map[string]any)
	if rs["slot"] == nil || rs["listed"] == nil || rs["Number"] != nil || rs["Dir"] != nil || rs["Runner"] != nil {
		t.Errorf("RunnerState = %v", rs)
	}
}
