package dashboard

import "net/http"

// Command describes one runnable gh nibfin command: the schema the
// Dashboard builds the command's input form from. The CLI builds it from its
// own command tree, so the CLI definition is its only source.
type Command struct {
	// Path is the command's names below the root, such as "runner add".
	Path string `json:"path"`
	// API is the command's name in POST /api/commands/<api>/plan; empty when
	// the command has no API equivalent.
	API   string `json:"api,omitempty"`
	Short string `json:"short"`
	Long  string `json:"long"`
	Flags []Flag `json:"flags"`
	Args  []Arg  `json:"args"`
}

// Flag describes one of a command's own flags.
type Flag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand"`
	// Type is the flag's value type, such as "int", "bool" or "string".
	Type     string   `json:"type"`
	Default  string   `json:"default"`
	Usage    string   `json:"usage"`
	Required bool     `json:"required"`
	Allowed  []string `json:"allowed,omitempty"`
	// Param is the request field the flag fills in the plan route; empty for
	// a CLI-only flag.
	Param string `json:"param,omitempty"`
}

// Arg describes one positional argument, in order.
type Arg struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	// Repeatable means the argument takes one or more values; only the last
	// one may be.
	Repeatable bool     `json:"repeatable"`
	Allowed    []string `json:"allowed,omitempty"`
	// Param is the request field the argument fills in the plan route.
	Param string `json:"param,omitempty"`
}

// commands serves every command's schema. It reads nothing else.
func (s *server) commands(w http.ResponseWriter, _ *http.Request) {
	c := s.o.Commands
	if c == nil {
		c = []Command{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": c})
}
