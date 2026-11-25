package host

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// WSLConfig is how plans and results name the WSL settings file.
const WSLConfig = `%UserProfile%\.wslconfig`

const (
	idleKey   = "vmIdleTimeout"
	idleValue = "-1"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func wslConfigPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", errorf(ErrAutostart, "find the home directory: %v", err)
	}
	return filepath.Join(home, ".wslconfig"), nil
}

// readWSLConfig returns the file's text without its UTF-8 BOM; a missing
// file is empty. A file in another encoding is refused.
func readWSLConfig() (path string, text string, bom bool, exists bool, err error) {
	if path, err = wslConfigPath(); err != nil {
		return
	}
	b, rerr := os.ReadFile(path)
	if errors.Is(rerr, os.ErrNotExist) {
		return path, "", false, false, nil
	}
	if rerr != nil {
		return path, "", false, false, errorf(ErrAutostart, "read %s: %v", path, rerr)
	}
	if bom = bytes.HasPrefix(b, utf8BOM); bom {
		b = b[len(utf8BOM):]
	}
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}) || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return path, "", false, true, errorf(ErrAutostart, "%s is not UTF-8 text; edit it by hand: [wsl2] %s=%s", path, idleKey, idleValue)
	}
	return path, string(b), bom, true, nil
}

// wslLines splits text into lines that keep their line endings.
func wslLines(text string) []string {
	var out []string
	for text != "" {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			out = append(out, text)
			break
		}
		out = append(out, text[:i+1])
		text = text[i+1:]
	}
	return out
}

func sectionName(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "[") {
		return "", false
	}
	end := strings.IndexByte(t, ']')
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(t[1:end]), true
}

// idleKeyValue returns the value of a vmIdleTimeout line.
func idleKeyValue(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' || t[0] == ';' {
		return "", false
	}
	k, v, ok := strings.Cut(t, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(k), idleKey) {
		return "", false
	}
	return strings.TrimSpace(v), true
}

// wslSection returns the line range of the first [wsl2] section's body:
// header is its header's index (-1 when there is none) and end the index
// after its last line.
func wslSection(lines []string) (header, end int) {
	header = -1
	for i, l := range lines {
		name, ok := sectionName(l)
		if !ok {
			continue
		}
		if header >= 0 {
			return header, i
		}
		if strings.EqualFold(name, "wsl2") {
			header = i
		}
	}
	return header, len(lines)
}

// setIdleTimeout returns text with [wsl2] vmIdleTimeout=-1, every other line
// unchanged.
func setIdleTimeout(text string) (string, bool) {
	eol := "\r\n"
	if strings.Contains(text, "\n") && !strings.Contains(text, "\r\n") {
		eol = "\n"
	}
	lines := wslLines(text)
	header, end := wslSection(lines)
	if header < 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += eol
		}
		return text + "[wsl2]" + eol + idleKey + "=" + idleValue + eol, true
	}
	found, changed := false, false
	for i := header + 1; i < end; i++ {
		v, ok := idleKeyValue(lines[i])
		if !ok {
			continue
		}
		found = true
		if v != idleValue {
			ending := lines[i][len(strings.TrimRight(lines[i], "\r\n")):]
			lines[i] = idleKey + "=" + idleValue + ending
			changed = true
		}
	}
	if !found {
		if !strings.HasSuffix(lines[header], "\n") {
			lines[header] += eol
		}
		rest := append([]string{idleKey + "=" + idleValue + eol}, lines[header+1:]...)
		lines = append(lines[:header+1], rest...)
		changed = true
	}
	return strings.Join(lines, ""), changed
}

// IdleTimeoutSet reports whether %UserProfile%\.wslconfig sets
// [wsl2] vmIdleTimeout=-1.
func (h *Host) IdleTimeoutSet() (bool, error) {
	_, text, _, exists, err := readWSLConfig()
	if err != nil || !exists {
		return false, err
	}
	_, changed := setIdleTimeout(text)
	return !changed, nil
}

// EnsureIdleTimeout sets [wsl2] vmIdleTimeout=-1 in %UserProfile%\.wslconfig,
// keeping every other line, the line endings and a UTF-8 BOM, and creating
// the file or the section when missing. It writes only when the key is not
// set already.
func (h *Host) EnsureIdleTimeout() (bool, error) {
	path, text, bom, _, err := readWSLConfig()
	if err != nil {
		return false, err
	}
	out, changed := setIdleTimeout(text)
	if !changed {
		return false, nil
	}
	if bom {
		out = string(utf8BOM) + out
	}
	if err := writeAtomic(path, []byte(out), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

var dottedVersion = regexp.MustCompile(`[0-9]+(\.[0-9]+)+`)

// decodeConsole decodes wsl.exe's output, which is UTF-16LE.
func decodeConsole(b []byte) string {
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.IndexByte(b, 0) >= 0 {
		b = bytes.TrimPrefix(b, []byte{0xFF, 0xFE})
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return string(utf16.Decode(u))
	}
	return strings.TrimPrefix(string(b), "\uFEFF")
}

// WSLVersion returns the WSL version from the first line of
// wsl.exe --version.
func (h *Host) WSLVersion(ctx context.Context) (string, error) {
	r, err := run(ctx, nil, "wsl.exe", "--version")
	if err != nil {
		if ctx.Err() != nil {
			return "", err
		}
		return "", errorf(ErrUnsupported, "wsl.exe --version: %v; WSL 2 or later is needed for [wsl2] %s: run `wsl --update`", err, idleKey)
	}
	out := strings.TrimSpace(decodeConsole([]byte(r.stdout)))
	v := dottedVersion.FindString(firstLine(out))
	if r.exit != 0 || v == "" {
		shown := firstLine(out)
		if shown == "" {
			shown = "exit status " + strconv.Itoa(r.exit)
		}
		return "", errorf(ErrUnsupported, "WSL (wsl --version: %q) does not support [wsl2] %s; run `wsl --update`", shown, idleKey)
	}
	return v, nil
}

// CheckWSL refuses a WSL older than 2.x, which has no vmIdleTimeout.
func (h *Host) CheckWSL(ctx context.Context) error {
	v, err := h.WSLVersion(ctx)
	if err != nil {
		return err
	}
	major, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	if major < 2 {
		return errorf(ErrUnsupported, "WSL %s does not support [wsl2] %s; run `wsl --update`", v, idleKey)
	}
	return nil
}
