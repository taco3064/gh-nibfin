package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/op"
)

func testRoot() *cobra.Command {
	var j bool
	return newRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, &j)
}

// runnable returns every available runnable command below root by path.
func runnable(root *cobra.Command) map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c != root && c.IsAvailableCommand() && c.Runnable() {
			out[strings.TrimPrefix(c.CommandPath(), root.CommandPath()+" ")] = c
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return out
}

// TestEveryCommandDeclaresItsArgs walks the whole tree: every runnable
// command declares its positional arguments, and the validation cobra runs
// agrees with the declaration.
func TestEveryCommandDeclaresItsArgs(t *testing.T) {
	cmds := runnable(testRoot())
	if len(cmds) == 0 {
		t.Fatal("no runnable command")
	}
	for path, c := range cmds {
		decl, ok := declaredArgs(c)
		if !ok {
			t.Errorf("%s: no %s declaration (use declareArgs)", path, argsAnnotation)
			continue
		}
		if c.Args == nil {
			t.Errorf("%s: no Args validation", path)
			continue
		}
		var allowed []string
		optional := false
		for i, a := range decl {
			if a.Name == "" {
				t.Errorf("%s: argument %d has no name", path, i)
			}
			if a.Required && optional {
				t.Errorf("%s: required %s follows an optional argument", path, a.Name)
			}
			optional = optional || !a.Required
			if a.Repeatable && i != len(decl)-1 {
				t.Errorf("%s: only the last argument may be repeatable, not %s", path, a.Name)
			}
			if len(a.Allowed) > 0 && len(decl) != 1 {
				t.Errorf("%s: %s declares allowed values but is not the sole argument", path, a.Name)
			}
			allowed = append(allowed, a.Allowed...)
		}
		if !reflect.DeepEqual([]string(c.ValidArgs), allowed) {
			t.Errorf("%s: ValidArgs = %v, declared %v", path, c.ValidArgs, allowed)
		}

		lo, hi := argCounts(decl)
		sample := func(n int) []string {
			s := make([]string, n)
			for i := range s {
				s[i] = "1"
				if len(decl) == 0 {
					continue
				}
				if a := decl[min(i, len(decl)-1)]; len(a.Allowed) > 0 {
					s[i] = a.Allowed[0]
				}
			}
			return s
		}
		top := len(decl) + 1
		if hi < 0 {
			top = len(decl) + 2
		}
		for n := 0; n <= top; n++ {
			want := n >= lo && (hi < 0 || n <= hi)
			err := c.Args(c, sample(n))
			if (err == nil) != want {
				t.Errorf("%s with %d args: err %v, declared %d..%d", path, n, err, lo, hi)
			}
			if err != nil && op.Code(err) != op.CodeInvalidArgument {
				t.Errorf("%s with %d args: code %q, want %s", path, n, op.Code(err), op.CodeInvalidArgument)
			}
		}
		for i, a := range decl {
			if len(a.Allowed) == 0 {
				continue
			}
			args := sample(max(lo, i+1))
			args[i] = "not-" + a.Allowed[0]
			if err := c.Args(c, args); err == nil {
				t.Errorf("%s: %q accepted for %s, allowed %v", path, args[i], a.Name, a.Allowed)
			}
		}
	}
}

func TestCommandSchemaFilters(t *testing.T) {
	root := &cobra.Command{Use: "nibfin", Annotations: map[string]string{cobra.CommandDisplayNameAnnotation: "gh nibfin"}}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().Bool("global", false, "inherited")
	run := func(*cobra.Command, []string) {}
	group := &cobra.Command{Use: "group"}
	leaf := &cobra.Command{Use: "leaf <x>", Short: "s", Long: "l", Run: run}
	declareArgs(leaf, dashboard.Arg{Name: "x", Required: true, Allowed: []string{"a", "b"}, Param: "x"})
	declareAPI(leaf, "group-leaf")
	leaf.Flags().String("mode", "fast", "the mode")
	_ = leaf.MarkFlagRequired("mode")
	_ = leaf.Flags().SetAnnotation("mode", allowedAnnotation, []string{"fast", "slow"})
	declareParam(leaf, "mode", "mode")
	leaf.Flags().Bool("secret", false, "hidden")
	_ = leaf.Flags().MarkHidden("secret")
	leaf.Flags().IntP("n", "n", 1, "count")
	hidden := &cobra.Command{Use: "hidden", Hidden: true, Run: run}
	declareArgs(hidden)
	group.AddCommand(leaf)
	root.AddCommand(group, hidden)
	// Executing adds the help flag and the help command.
	root.SetArgs([]string{"group", "leaf", "a", "--mode", "slow"})
	root.SetOut(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	got := commandSchema(root)
	want := []dashboard.Command{{
		Path:  "group leaf",
		API:   "group-leaf",
		Short: "s",
		Long:  "l",
		Flags: []dashboard.Flag{
			{Name: "mode", Type: "string", Default: "fast", Usage: "the mode", Required: true, Allowed: []string{"fast", "slow"}, Param: "mode"},
			{Name: "n", Shorthand: "n", Type: "int", Default: "1", Usage: "count"},
		},
		Args: []dashboard.Arg{{Name: "x", Required: true, Allowed: []string{"a", "b"}, Param: "x"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("schema = %+v\nwant %+v", got, want)
	}
}

func TestCommandSchemaOfTheCLI(t *testing.T) {
	schema := commandSchema(testRoot())
	byPath := map[string]dashboard.Command{}
	var paths []string
	for _, c := range schema {
		byPath[c.Path] = c
		paths = append(paths, c.Path)
		for _, f := range c.Flags {
			if f.Name == "help" {
				t.Errorf("%s lists the help flag", c.Path)
			}
		}
	}
	// In the order cobra lists the tree, as --help does.
	want := []string{"dashboard", "init", "login", "pause", "resume", "runner", "runner add", "runner remove", "status", "upgrade"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}

	add := byPath["runner add"]
	wantFlags := []dashboard.Flag{
		{Name: "count", Type: "int", Default: "2", Usage: "number of Runners", Param: "count"},
		{Name: "yes", Type: "bool", Default: "false", Usage: "apply the plan without asking to confirm"},
	}
	if add.API != "runner-add" || !reflect.DeepEqual(add.Flags, wantFlags) {
		t.Errorf("runner add = %+v", add)
	}
	if want := []dashboard.Arg{{Name: "provider", Required: true, Allowed: []string{"claude", "codex"}, Param: "provider"}}; !reflect.DeepEqual(add.Args, want) {
		t.Errorf("runner add args = %+v", add.Args)
	}
	if !strings.HasPrefix(add.Long, "Add Runners") || add.Short == "" {
		t.Errorf("runner add texts = %q, %q", add.Short, add.Long)
	}

	pause := byPath["pause"]
	if want := []dashboard.Arg{{Name: "issue", Required: true, Repeatable: true, Param: "issues"}}; !reflect.DeepEqual(pause.Args, want) {
		t.Errorf("pause args = %+v", pause.Args)
	}
	if len(pause.Flags) != 1 || pause.Flags[0].Name != "repo" || pause.Flags[0].Shorthand != "R" || pause.Flags[0].Param != "repo" {
		t.Errorf("pause flags = %+v", pause.Flags)
	}
	if d := byPath["dashboard"]; d.API != "" || len(d.Args) != 0 {
		t.Errorf("dashboard = %+v", d)
	}
	if r := byPath["runner"]; r.API != "" || r.Args == nil || len(r.Args) != 0 {
		t.Errorf("runner = %+v", r)
	}
}

// TestSchemaNamesMatchThePlanRoute posts every schema command with an API
// name to the real server, filling every declared param, and checks the
// server accepts the names and that they fill every API field.
func TestSchemaNamesMatchThePlanRoute(t *testing.T) {
	fakeRepo(t)
	got := map[string]any{}
	rec := func(name string, p any) op.Operation { got[name] = p; return recordingOp{} }
	fake := dashboard.Ops{
		RunnerAdd: func(p op.RunnerAddParams) op.Operation {
			return rec("RunnerAdd", op.RunnerAddParams{Provider: p.Provider, Count: p.Count})
		},
		RunnerRemove: func(p op.RunnerRemoveParams) op.Operation { return rec("RunnerRemove", p) },
		Login:        func(p op.LoginParams) op.Operation { return rec("Login", op.LoginParams{Provider: p.Provider}) },
		Init:         func(p op.RepoParams) op.Operation { return rec("Init", p) },
		Upgrade:      func(p op.RepoParams) op.Operation { return rec("Upgrade", p) },
		Status:       func(p op.RepoParams) op.Operation { return rec("Status", p) },
		Pause:        func(p op.IssuesParams) op.Operation { return rec("Pause", p) },
		Resume:       func(p op.ResumeParams) op.Operation { return rec("Resume", p) },
	}
	schema := commandSchema(testRoot())

	ready := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- dashboard.Serve(ctx, dashboard.Options{
			Ops: fake, ResolveRepo: repoResolver, Commands: schema,
			Ready: func(u string) { ready <- u },
		})
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve = %v", err)
		}
	}()
	base, proof, _ := strings.Cut(<-ready, "/#proof=")
	do := func(method, path, session, body string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		if session == "" {
			req.Header.Set(dashboard.ProofHeader, proof)
		} else {
			req.Header.Set(dashboard.SessionHeader, session)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	_, b := do("POST", "/api/session", "", "")
	var s struct{ Session string }
	if json.Unmarshal(b, &s) != nil || s.Session == "" {
		t.Fatalf("exchange: %s", b)
	}

	// The served schema is the CLI's.
	status, b := do("GET", "/api/commands", s.Session, "")
	var served struct{ Commands []dashboard.Command }
	if status != http.StatusOK || json.Unmarshal(b, &served) != nil || !reflect.DeepEqual(served.Commands, schema) {
		t.Errorf("GET /api/commands: %d %s", status, b)
	}

	sample := func(typ string, allowed []string) any {
		switch {
		case len(allowed) > 0:
			return allowed[0]
		case typ == "int":
			return 3
		case typ == "bool":
			return true
		}
		return "o/r"
	}
	apis := 0
	for _, c := range schema {
		if c.API == "" {
			continue
		}
		apis++
		body := map[string]any{}
		for _, f := range c.Flags {
			if f.Param != "" {
				body[f.Param] = sample(f.Type, f.Allowed)
			}
		}
		for _, a := range c.Args {
			v := sample("string", a.Allowed)
			if a.Param == "" {
				t.Errorf("%s: argument %s has no param", c.Path, a.Name)
				continue
			}
			if a.Repeatable {
				v = []any{"7"}
			}
			body[a.Param] = v
		}
		raw, _ := json.Marshal(body)
		status, b := do("POST", "/api/commands/"+c.API+"/plan", s.Session, string(raw))
		var e struct{ Error struct{ Code string } }
		_ = json.Unmarshal(b, &e)
		// recordingOp stops at Plan with its own code, so the entry was reached.
		if status != http.StatusUnprocessableEntity || e.Error.Code != "stop" {
			t.Errorf("%s %s: %d %s", c.Path, raw, status, b)
		}
	}
	if apis != reflect.TypeOf(fake).NumField() {
		t.Errorf("%d schema commands have an api, Ops has %d entries", apis, reflect.TypeOf(fake).NumField())
	}
	for i := 0; i < reflect.TypeOf(fake).NumField(); i++ {
		name := reflect.TypeOf(fake).Field(i).Name
		p, ok := got[name]
		if !ok {
			t.Errorf("no schema command reached Ops.%s", name)
			continue
		}
		v := reflect.ValueOf(p)
		for j := 0; j < v.NumField(); j++ {
			f := v.Type().Field(j)
			if tag := f.Tag.Get("json"); tag != "-" && v.Field(j).IsZero() {
				t.Errorf("Ops.%s: no schema input fills %s", name, f.Name)
			}
		}
	}
}
