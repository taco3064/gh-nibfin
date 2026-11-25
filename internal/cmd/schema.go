package cmd

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
)

// Annotation keys of the command schema. A command declares its positional
// arguments under argsAnnotation and its plan route name under
// apiAnnotation; a flag declares its request field under paramAnnotation and
// its allowed values, when it has any, under allowedAnnotation.
const (
	argsAnnotation    = "nibfin:args"
	apiAnnotation     = "nibfin:api"
	paramAnnotation   = "nibfin:param"
	allowedAnnotation = "nibfin:allowed"
)

// declareArgs declares c's positional arguments, in order, and derives
// c.Args and c.ValidArgs from them, so the validation the CLI runs and the
// schema the Dashboard reads come from one declaration. A command without
// positional arguments calls it with none.
func declareArgs(c *cobra.Command, args ...dashboard.Arg) {
	if args == nil {
		args = []dashboard.Arg{}
	}
	b, err := json.Marshal(args)
	if err != nil {
		panic(err) // Arg holds only strings and bools.
	}
	setAnnotation(c, argsAnnotation, string(b))

	min, max := argCounts(args)
	var n cobra.PositionalArgs
	switch {
	case max == 0:
		n = cobra.NoArgs
	case max < 0:
		n = cobra.MinimumNArgs(min)
	case min == max:
		n = cobra.ExactArgs(min)
	default:
		n = cobra.RangeArgs(min, max)
	}
	c.ValidArgs = nil
	for _, a := range args {
		c.ValidArgs = append(c.ValidArgs, a.Allowed...)
	}
	if len(c.ValidArgs) > 0 {
		n = cobra.MatchAll(n, cobra.OnlyValidArgs)
	}
	c.Args = wrapArgs(n)
}

// argCounts returns the least and most positional values args accept; max
// is -1 when the last argument is repeatable.
func argCounts(args []dashboard.Arg) (min, max int) {
	for _, a := range args {
		if a.Required {
			min++
		}
	}
	max = len(args)
	if max > 0 && args[max-1].Repeatable {
		max = -1
	}
	return min, max
}

// declareAPI names c's command in the Dashboard's plan route.
func declareAPI(c *cobra.Command, name string) { setAnnotation(c, apiAnnotation, name) }

// declareParam names the plan request field the flag fills.
func declareParam(c *cobra.Command, flag, field string) {
	if err := c.Flags().SetAnnotation(flag, paramAnnotation, []string{field}); err != nil {
		panic(err) // the flag is defined just before
	}
}

func setAnnotation(c *cobra.Command, key, value string) {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[key] = value
}

// declaredArgs decodes c's positional declaration; ok is false when c has
// none or it does not decode.
func declaredArgs(c *cobra.Command) (args []dashboard.Arg, ok bool) {
	v, ok := c.Annotations[argsAnnotation]
	if !ok || json.Unmarshal([]byte(v), &args) != nil {
		return nil, false
	}
	return args, true
}

// commandSchema walks root's tree and describes every available runnable
// command, in the order cobra lists the tree (as --help does).
func commandSchema(root *cobra.Command) []dashboard.Command {
	out := []dashboard.Command{}
	var walk func(c *cobra.Command, names []string)
	walk = func(c *cobra.Command, names []string) {
		if c != root && c.IsAvailableCommand() && c.Runnable() {
			out = append(out, describe(c, strings.Join(names, " ")))
		}
		for _, sub := range c.Commands() {
			if sub.IsAvailableCommand() {
				walk(sub, append(names[:len(names):len(names)], sub.Name()))
			}
		}
	}
	walk(root, nil)
	return out
}

func describe(c *cobra.Command, path string) dashboard.Command {
	d := dashboard.Command{
		Path:  path,
		API:   c.Annotations[apiAnnotation],
		Short: c.Short,
		Long:  c.Long,
		Flags: []dashboard.Flag{},
		Args:  []dashboard.Arg{},
	}
	if args, ok := declaredArgs(c); ok {
		d.Args = args
	}
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" || f.Annotations[cobra.FlagSetByCobraAnnotation] != nil {
			return
		}
		fl := dashboard.Flag{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Type:      f.Value.Type(),
			Default:   f.DefValue,
			Usage:     f.Usage,
			Required:  len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0 && f.Annotations[cobra.BashCompOneRequiredFlag][0] == "true",
			Allowed:   f.Annotations[allowedAnnotation],
		}
		if p := f.Annotations[paramAnnotation]; len(p) > 0 {
			fl.Param = p[0]
		}
		d.Flags = append(d.Flags, fl)
	})
	return d
}
