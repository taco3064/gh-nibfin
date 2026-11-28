package repair

import (
	"regexp"
	"strconv"
	"strings"
)

// Text is the visible part of a repair record (R4.49) as Core writes it.
type Text struct {
	Category string
	// Reason is one line.
	Reason    string
	Done      string
	Advice    string
	Options   []Option
	Steps     []Step
	BugReport *BugReport
}

// Option is one numbered option of a waiting-for-choice record.
type Option struct {
	Number      int
	Title       string
	Consequence string
	IssueChange string
	Recommended bool
}

// Step is one numbered follow-up step: a Link or a Command, or neither.
type Step struct {
	Text    string
	Link    string
	Command string
}

// BugReport is what to file at Nibfin for a Nibfin process error.
type BugReport struct {
	URL   string
	Title string
	Body  string
}

// indent is the indentation of an item's lines (Core's indent()).
const indent = "   "

var (
	optionItem = regexp.MustCompile(`^([1-9][0-9]*)\. \*\*(.*)\*\*( \(recommended\))?$`)
	stepItem   = regexp.MustCompile(`^([1-9][0-9]*)\. (.*)$`)
	fenceOpen  = regexp.MustCompile("^(`{3,})[^`]*$")
)

// ParseText reads the visible lines of a record body: Category, Reason, Done
// and Advice by their "- X: " prefix before the first section, then the
// Options, Steps and Bug report sections. What does not parse is left out.
func ParseText(body string) Text {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var t Text
	sections := map[string][]string{}
	var head []string
	cur := ""
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		// A fence at the start of a line (the bug report body) is one
		// block: a heading inside it is text.
		if m := fenceOpen.FindStringSubmatch(l); m != nil {
			end := i + 1
			for end < len(lines) && lines[end] != m[1] {
				end++
			}
			if end == len(lines) {
				end--
			}
			block := lines[i : end+1]
			if cur == "" {
				head = append(head, block...)
			} else {
				sections[cur] = append(sections[cur], block...)
			}
			i = end
			continue
		}
		if h, ok := strings.CutPrefix(l, "### "); ok {
			cur = strings.TrimSpace(h)
			continue
		}
		if cur == "" {
			head = append(head, l)
		} else {
			sections[cur] = append(sections[cur], l)
		}
	}
	t.Category = field(head, "Category")
	t.Reason = field(head, "Reason")
	t.Done = field(head, "Done")
	t.Advice = field(head, "Advice")
	t.Options = parseOptions(sections["Options"])
	t.Steps = parseSteps(sections["Steps"])
	t.BugReport = parseBugReport(sections["Bug report for Nibfin"])
	return t
}

// field is the value of the first top-level "- name: " item, with the
// lines that continue it (not blank, not another top-level item and not
// indented as a nested item).
func field(lines []string, name string) string {
	prefix := "- " + name + ": "
	for i, l := range lines {
		v, ok := strings.CutPrefix(l, prefix)
		if !ok {
			continue
		}
		out := []string{v}
		for _, c := range lines[i+1:] {
			if strings.TrimSpace(c) == "" || strings.HasPrefix(c, "- ") || strings.HasPrefix(strings.TrimLeft(c, " "), "- ") {
				break
			}
			out = append(out, strings.TrimSpace(c))
		}
		return strings.TrimSpace(strings.Join(out, " "))
	}
	return ""
}

// items splits a section into numbered items: each item line that re
// accepts starts one, with the lines after it up to the next.
func items(lines []string, re *regexp.Regexp) [][]string {
	var out [][]string
	for _, l := range lines {
		if re.MatchString(l) {
			out = append(out, []string{l})
		} else if len(out) > 0 {
			out[len(out)-1] = append(out[len(out)-1], l)
		}
	}
	return out
}

// dedent removes the item indentation of lines; a blank line stays blank,
// and a line without the indentation ends the item's body.
func dedent(lines []string) []string {
	var out []string
	for _, l := range lines {
		switch {
		case l == "":
			out = append(out, "")
		case strings.HasPrefix(l, indent):
			out = append(out, strings.TrimPrefix(l, indent))
		default:
			return trimBlank(out)
		}
	}
	return trimBlank(out)
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return lines
}

func parseOptions(lines []string) []Option {
	var out []Option
	for _, it := range items(lines, optionItem) {
		m := optionItem.FindStringSubmatch(it[0])
		n, _ := strconv.Atoi(m[1])
		o := Option{Number: n, Title: m[2], Recommended: m[3] != ""}
		body := dedent(it[1:])
		change := len(body)
		for i, l := range body {
			if strings.HasPrefix(l, "Issue change: ") {
				change = i
				break
			}
		}
		o.Consequence = strings.Join(trimBlank(body[:change]), "\n")
		if change < len(body) {
			rest := append([]string{strings.TrimPrefix(body[change], "Issue change: ")}, body[change+1:]...)
			o.IssueChange = strings.Join(trimBlank(rest), "\n")
		}
		out = append(out, o)
	}
	return out
}

func parseSteps(lines []string) []Step {
	var out []Step
	for _, it := range items(lines, stepItem) {
		m := stepItem.FindStringSubmatch(it[0])
		s := Step{Text: m[2]}
		body := dedent(it[1:])
		for i := 0; i < len(body); i++ {
			if v, ok := strings.CutPrefix(body[i], "Link: "); ok && s.Link == "" {
				s.Link = strings.TrimSpace(v)
				continue
			}
			if f := fenceOpen.FindStringSubmatch(body[i]); f != nil && s.Command == "" {
				end := i + 1
				for end < len(body) && body[end] != f[1] {
					end++
				}
				if end < len(body) {
					s.Command = strings.Join(body[i+1:end], "\n")
				}
				i = end
			}
		}
		out = append(out, s)
	}
	return out
}

func parseBugReport(lines []string) *BugReport {
	if len(lines) == 0 {
		return nil
	}
	var b BugReport
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if v, ok := strings.CutPrefix(l, "File it at "); ok && b.URL == "" {
			b.URL, _, _ = strings.Cut(v, " ")
			continue
		}
		if v, ok := strings.CutPrefix(l, "Title: "); ok && b.Title == "" {
			b.Title = strings.TrimSpace(v)
			continue
		}
		if f := fenceOpen.FindStringSubmatch(l); f != nil && b.Body == "" {
			end := i + 1
			for end < len(lines) && lines[end] != f[1] {
				end++
			}
			if end < len(lines) {
				b.Body = strings.Join(lines[i+1:end], "\n")
			}
			i = end
		}
	}
	if b.URL == "" && b.Title == "" && b.Body == "" {
		return nil
	}
	return &b
}

// ValidOptions reports whether options are what a waiting-for-choice
// record carries: 2–4 options numbered 1..n with a title, exactly one of
// them recommended.
func ValidOptions(options []Option) bool {
	if len(options) < 2 || len(options) > 4 {
		return false
	}
	recommended := 0
	for i, o := range options {
		if o.Number != i+1 || strings.TrimSpace(o.Title) == "" {
			return false
		}
		if o.Recommended {
			recommended++
		}
	}
	return recommended == 1
}
