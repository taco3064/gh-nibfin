package templates

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type workflowStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Env  map[string]string `yaml:"env"`
	Run  string            `yaml:"run"`
}

type workflowDoc struct {
	Name        string         `yaml:"name"`
	RunName     string         `yaml:"run-name"`
	On          map[string]any `yaml:"on"`
	Permissions any            `yaml:"permissions"`
	Jobs        map[string]struct {
		If     string            `yaml:"if"`
		RunsOn any               `yaml:"runs-on"`
		Env    map[string]string `yaml:"env"`
		Steps  []workflowStep    `yaml:"steps"`
	} `yaml:"jobs"`
}

func parseWorkflow(t *testing.T, b []byte) workflowDoc {
	t.Helper()
	var d workflowDoc
	if err := yaml.Unmarshal(b, &d); err != nil {
		t.Fatalf("parse: %v\n%s", err, b)
	}
	return d
}

func requestWorkflow(t *testing.T) (workflowDoc, string) {
	t.Helper()
	b := render(t, Execution, params())[".github/workflows/"+RunnerRequestFile]
	if b == nil {
		t.Fatalf("the execution set has no %s", RunnerRequestFile)
	}
	return parseWorkflow(t, b), string(b)
}

func step(t *testing.T, d workflowDoc, name string) workflowStep {
	t.Helper()
	for _, s := range d.Jobs["request"].Steps {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no step %q", name)
	return workflowStep{}
}

// TestRenderRunsUseEnvOnly checks that no run script of any template
// interpolates an expression: event text, inputs and repository variables
// reach commands only as quoted environment variables. The collaborator's
// runner hook relies on it.
func TestRenderRunsUseEnvOnly(t *testing.T) {
	for name, b := range render(t, Execution, params()) {
		d := parseWorkflow(t, b)
		runs := 0
		for job, j := range d.Jobs {
			for i, s := range j.Steps {
				if s.Run == "" {
					continue
				}
				runs++
				if strings.Contains(s.Run, "${{") {
					t.Errorf("%s: job %s step %d (%s) interpolates an expression in run", name, job, i, s.Name)
				}
			}
		}
		if runs == 0 && !strings.HasSuffix(name, "nibfin-shared-runner.yml") {
			t.Errorf("%s: no run scripts parsed", name)
		}
		if strings.Contains(string(b), "vars.NIBFIN_PROVIDER") {
			t.Errorf("%s reads the NIBFIN_PROVIDER variable", name)
		}
		if strings.Contains(string(b), "vars.NIBFIN_MODEL") {
			t.Errorf("%s reads the NIBFIN_MODEL variable", name)
		}
	}
}

func TestRenderRunnerRequest(t *testing.T) {
	d, text := requestWorkflow(t)
	if d.Name != "Nibfin · Runner request" || d.RunName != "Nibfin · ${{ github.repository }}#${{ github.event.issue.number }} · runner request" {
		t.Errorf("name %q, run-name %q", d.Name, d.RunName)
	}
	issues, _ := d.On["issues"].(map[string]any)
	if len(d.On) != 1 || issues == nil || !slices.Equal(anyStrings(issues["types"]), []string{"opened"}) {
		t.Errorf("on = %v, want issues opened only", d.On)
	}
	if perms, _ := d.Permissions.(map[string]any); len(perms) != 1 || perms["issues"] != "write" {
		t.Errorf("permissions = %v, want issues: write only", d.Permissions)
	}
	job, ok := d.Jobs["request"]
	if !ok || len(d.Jobs) != 1 || job.RunsOn != "ubuntu-24.04" {
		t.Fatalf("jobs %+v, want one request job on ubuntu-24.04", d.Jobs)
	}
	if want := "contains(github.event.issue.body, '" + RunnerRequestMarker + "')"; job.If != want {
		t.Errorf("job if %q, want %q", job.If, want)
	}
	if job.Env["GH_TOKEN"] != "${{ github.token }}" {
		t.Errorf("GH_TOKEN %q, want the job's token", job.Env["GH_TOKEN"])
	}
	if m := usesLine.FindAllString(text, -1); len(m) != 0 {
		t.Errorf("the request workflow uses %q", m)
	}
	if strings.Contains(text, "set -x") || strings.Contains(text, "set -o xtrace") {
		t.Error("the request workflow traces commands")
	}

	// The Nibfin token reaches only the check, deploy key and token steps, as env.
	if n := strings.Count(text, "secrets."); n != 3 {
		t.Errorf("secrets appear %d times, want 3", n)
	}
	for _, s := range job.Steps {
		want := s.Name == "Check the request" || s.Name == "Add the deploy key" || s.Name == "Create and encrypt the registration token"
		if got := s.Env["NIBFIN_TOKEN"] == "${{ secrets.NIBFIN_GITHUB_TOKEN }}"; got != want {
			t.Errorf("step %q has the Nibfin token: %v", s.Name, got)
		}
	}

	check := step(t, d, "Check the request")
	if check.ID != "check" || !strings.Contains(check.Run, `if [ "$TITLE" != "`+RunnerRequestTitle+`" ]; then`) ||
		!strings.Contains(check.Run, `'^`+RunnerKeyIDField+`:'`) ||
		!strings.Contains(check.Run, `GH_TOKEN="$NIBFIN_TOKEN" gh_retry api "repos/$REPO/collaborators/$LOGIN/permission" --jq .permission`) {
		t.Errorf("the check step does not check the contract:\n%s", check.Run)
	}
	for k, v := range map[string]string{"TITLE": "github.event.issue.title", "BODY": "github.event.issue.body", "USER_TYPE": "github.event.issue.user.type"} {
		if check.Env[k] != "${{ "+v+" }}" {
			t.Errorf("check env %s = %q", k, check.Env[k])
		}
	}
	if job.Env["LOGIN"] != "${{ github.event.issue.user.login }}" {
		t.Errorf("the requester is not the Issue's author: %q", job.Env["LOGIN"])
	}
	if s := step(t, d, "Install age"); s.If != "steps.check.outputs.state == 'ok'" || !strings.Contains(s.Run, "sudo apt-get install -y age") {
		t.Errorf("install step %+v", s)
	}

	// The token is masked before any other use and reaches age on stdin only.
	tok := step(t, d, "Create and encrypt the registration token")
	if !strings.Contains(tok.Run, `GH_TOKEN="$NIBFIN_TOKEN" gh_retry api -X POST "repos/$REPO/actions/runners/registration-token"`) {
		t.Errorf("the token step does not create the token with the Nibfin token:\n%s", tok.Run)
	}
	var uses []string
	for _, l := range strings.Split(tok.Run, "\n") {
		if strings.Contains(l, "$token") {
			uses = append(uses, strings.TrimSpace(l))
		}
	}
	wantUses := []string{
		`if [ -z "$token" ]; then`,
		`echo "::add-mask::$token"`,
		`ciphertext=$(printf '%s' "$token" | age -a -R "$RUNNER_TEMP/nibfin-request.pub")`,
	}
	if !slices.Equal(uses, wantUses) {
		t.Errorf("token uses %q, want %q", uses, wantUses)
	}
	for _, want := range []string{
		"echo '" + RunnerRegistrationMarker + "'",
		`echo "` + RunnerRequesterField + `: $LOGIN"`,
		`echo "` + RunnerKeyIDField + `: $KEY_ID"`,
		`echo "` + RunnerExpiresField + `: $expires"`,
		"echo '```text'",
	} {
		if !strings.Contains(tok.Run, want) {
			t.Errorf("the registration comment has no %q", want)
		}
	}

	answer := step(t, d, "Answer the request")
	if answer.If != "${{ !cancelled() && steps.check.outputs.state != 'closed' }}" || answer.Env["JOB_STATUS"] != "${{ job.status }}" {
		t.Errorf("answer step if %q, env %v", answer.If, answer.Env)
	}
	for _, want := range []string{
		"marker='" + RunnerRefusedMarker + "'",
		"marker='" + RunnerRegistrationMarker + "'",
		`if [ "$marker" = '` + RunnerRefusedMarker + `' ]; then`,
		`gh_retry api -X PATCH "repos/$REPO/issues/$ISSUE" -f state=closed -f state_reason=not_planned`,
		`gh api -X PATCH "repos/$REPO/issues/comments/$id"`,
		`select(.user.login == "github-actions[bot]" and (.body | startswith($m)))`,
		RunnerReasonField + `: the request workflow failed (%s)`,
	} {
		if !strings.Contains(answer.Run, want) {
			t.Errorf("the answer step has no %q", want)
		}
	}
	if !strings.Contains(check.Run, `printf '`+RunnerReasonField+`: %s\n\n%s\n' "$1" "$2"`) {
		t.Error("the refusal has no reason line")
	}

	// Every marker in the workflow is one of the contract's.
	markers := regexp.MustCompile(`<!-- nibfin:[a-z-]+ -->`).FindAllString(text, -1)
	for _, m := range markers {
		if m != RunnerRequestMarker && m != RunnerRegistrationMarker && m != RunnerRefusedMarker {
			t.Errorf("marker %q is not in the contract", m)
		}
	}
	for _, m := range []string{RunnerRequestMarker, RunnerRegistrationMarker, RunnerRefusedMarker} {
		if !slices.Contains(markers, m) {
			t.Errorf("the workflow does not use %q", m)
		}
	}
}

func anyStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, _ := it.(string)
		out = append(out, s)
	}
	return out
}

func needBash(t *testing.T, tools ...string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the workflow scripts run on Linux runners")
	}
	for _, tool := range append([]string{"bash"}, tools...) {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
}

// runScript runs script as GitHub runs a bash step.
func runScript(t *testing.T, script string, env []string) (string, error) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "step.sh")
	if err := os.WriteFile(f, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("bash", "--noprofile", "--norc", "-eo", "pipefail", f)
	// sleep is a no-op that logs its seconds, so retries do not wait.
	c.Env = append([]string{`BASH_FUNC_sleep%%=() { echo "sleep $*" >&2; }`}, env...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// TestJobProviderCheck runs the start of the stage script: the provider is
// the Runner's NIBFIN_PROVIDER, and any other value stops the Job.
func TestJobProviderCheck(t *testing.T) {
	needBash(t)
	d := parseWorkflow(t, render(t, Execution, params())[".github/workflows/nibfin-job.yml"])
	var script string
	for _, s := range d.Jobs["stage"].Steps {
		if s.Name == "Run the stage" {
			script = s.Run
			if _, ok := s.Env["PROVIDER"]; ok {
				t.Error("the stage step still sets PROVIDER from the workflow")
			}
		}
	}
	head, _, ok := strings.Cut(script, "mkdir -p")
	if !ok || !strings.HasPrefix(head, "# The Runner's .env sets NIBFIN_PROVIDER") {
		t.Fatalf("the stage script does not start with the provider check:\n%s", script)
	}
	head += `printf 'provider=%s' "$PROVIDER"` + "\n"
	for _, p := range []string{"claude", "codex"} {
		out, err := runScript(t, head, []string{"PATH=" + os.Getenv("PATH"), "NIBFIN_PROVIDER=" + p})
		if err != nil || out != "provider="+p {
			t.Errorf("%s: %v, %q", p, err, out)
		}
	}
	for _, env := range [][]string{{}, {"NIBFIN_PROVIDER="}, {"NIBFIN_PROVIDER=gemini"}, {"NIBFIN_PROVIDER=claude "}} {
		out, err := runScript(t, head, append([]string{"PATH=" + os.Getenv("PATH")}, env...))
		if err == nil || !strings.Contains(out, "This Runner's .env must set NIBFIN_PROVIDER to claude or codex") || strings.Contains(out, "provider=") {
			t.Errorf("%v: %v, %q", env, err, out)
		}
	}
}

// TestJobModel runs the stage script up to the container limits: the model is
// the Runner's NIBFIN_MODEL, passed as --model only when it is set, and no
// workflow env can set it.
func TestJobModel(t *testing.T) {
	needBash(t)
	d := parseWorkflow(t, render(t, Execution, params())[".github/workflows/nibfin-job.yml"])
	var script string
	for _, s := range d.Jobs["stage"].Steps {
		if s.Name == "Run the stage" {
			script = s.Run
			for _, k := range []string{"MODEL", "NIBFIN_MODEL"} {
				if _, ok := s.Env[k]; ok {
					t.Errorf("the stage step sets %s from the workflow", k)
				}
			}
		}
	}
	if _, ok := d.Jobs["stage"].Env["NIBFIN_MODEL"]; ok {
		t.Error("the stage job sets NIBFIN_MODEL from the workflow")
	}
	head, _, ok := strings.Cut(script, "# NIBFIN_CONTAINER_LIMITS")
	if !ok || !strings.Contains(head, "options=(") {
		t.Fatalf("the stage script does not build its options before the limits:\n%s", script)
	}
	head += `printf '%s\n' "${options[@]}"` + "\n"
	base := []string{"PATH=" + os.Getenv("PATH"), "RUNNER_TEMP=" + t.TempDir(),
		"NIBFIN_PROVIDER=codex", "STAGE=deliver", "REPO=o/r", "ISSUE=7"}
	for _, c := range []struct {
		env  []string
		want string
	}{
		{[]string{"NIBFIN_MODEL=some model"}, "--stage\ndeliver\n--repo\no/r\n--issue\n7\n--provider\ncodex\n--model\nsome model\n"},
		{nil, "--stage\ndeliver\n--repo\no/r\n--issue\n7\n--provider\ncodex\n"},
		{[]string{"NIBFIN_MODEL="}, "--stage\ndeliver\n--repo\no/r\n--issue\n7\n--provider\ncodex\n"},
		{[]string{"MODEL=from-workflow"}, "--stage\ndeliver\n--repo\no/r\n--issue\n7\n--provider\ncodex\n"},
	} {
		out, err := runScript(t, head, append(append([]string{}, base...), c.env...))
		if err != nil || out != c.want {
			t.Errorf("%v: %v, %q", c.env, err, out)
		}
	}
}

// fakeGH answers the gh api calls of the check step from env and logs the
// token each call used.
const fakeGH = `#!/bin/bash
printf '%s|%s\n' "$GH_TOKEN" "$*" >>"$GH_LOG"
case "$*" in
  "api repos/o/r/issues/7 --jq .state") echo "$FAKE_STATE" ;;
  "api repos/o/r/collaborators/alice/permission --jq .permission") echo "$FAKE_PERMISSION" ;;
  "api --paginate users/alice/keys") printf '%s\n' "$FAKE_KEYS" ;;
  "api -X POST repos/o/r/keys -f title=nibfin-runner-alice -f key=ssh-ed25519 "*" -F read_only=true --jq .id")
    if [ "$FAKE_KEY_POST" = ok ]; then echo 99; else echo "gh: Validation Failed (HTTP 422)" >&2; exit 1; fi ;;
  "api --paginate repos/o/r/keys")
    if [ -n "$FAKE_KEYS_LIST_FAIL" ]; then echo "gh: Not Found (HTTP 404)" >&2; exit 1; fi
    printf '%s\n' "$FAKE_DEPLOY_KEYS" ;;
  "api -X POST repos/o/r/actions/runners/registration-token")
    printf '{"token":"%s","expires_at":"2026-11-25T13:00:00Z"}\n' "$FAKE_REG_TOKEN" ;;
  "api --paginate repos/o/r/issues/7/comments")
    if [ -f "$GH_DIR/comments.json" ]; then cat "$GH_DIR/comments.json"; else printf '%s\n' "$FAKE_COMMENTS"; fi ;;
  "api -X PATCH repos/o/r/issues/comments/"*" --input "*" --jq .html_url")
    id=${4##*/}
    cp "$6" "$GH_DIR/patch-$id.json"
    echo "https://github.com/o/r/issues/7#issuecomment-$id" ;;
  "api -X POST repos/o/r/issues/7/comments --input "*" --jq .html_url")
    cp "$6" "$GH_DIR/post.json"
    # FAKE_POST_LOST: the comment is created but the answer is lost.
    if [ -n "$FAKE_POST_LOST" ]; then
      jq -c '[{id: 6, user: {login: "github-actions[bot]"}, body: .body}]' "$6" >"$GH_DIR/comments.json"
      echo "gh: Bad Gateway (HTTP 502)" >&2
      exit 1
    fi
    echo "https://github.com/o/r/issues/7#issuecomment-new" ;;
  "api -X PATCH repos/o/r/issues/7 -f state=closed -f state_reason=not_planned --jq .state")
    touch "$GH_DIR/closed"
    echo closed ;;
  *) echo "unexpected gh $*" >&2; exit 1 ;;
esac
`

// TestRunnerRequestCheck runs the check step against a fake gh.
func TestRunnerRequestCheck(t *testing.T) {
	needBash(t, "grep", "sed")
	_, hasJQ := exec.LookPath("jq")
	d, _ := requestWorkflow(t)
	script := step(t, d, "Check the request").Run
	keys := `[{"id":11,"key":"ssh-ed25519 AAAAC3Nza alice@a"},{"id":12,"key":"ssh-rsa AAAAB3Nza"},{"id":13,"key":"ecdsa-sha2-nistp256 AAAAE2V"}]`
	body := func(lines ...string) string {
		return strings.Join(append([]string{RunnerRequestMarker}, lines...), "\r\n")
	}
	cases := []struct {
		name, title, body, userType, state, permission string
		jq                                             bool
		want, reason                                   string
	}{
		{name: "closed", state: "closed", want: "closed"},
		{name: "title", title: "Runner please", want: "refused", reason: `the title is not "Nibfin runner request"`},
		{name: "bot", userType: "Bot", want: "refused", reason: "the author alice is a Bot account"},
		{name: "no key line", body: body("hello"), want: "refused", reason: "the body has 0 ssh-key-id lines"},
		{name: "two key lines", body: body("ssh-key-id: 11", "ssh-key-id: 12"), want: "refused", reason: "the body has 2 ssh-key-id lines"},
		{name: "bad id", body: body("ssh-key-id: 011"), want: "refused", reason: `the ssh-key-id line is not "ssh-key-id: <id>"`},
		{name: "read", permission: "read", want: "refused", reason: "alice has read permission on o/r, not write or admin"},
		{name: "none", permission: "none", want: "refused", reason: "alice has none permission"},
		{name: "unknown key", body: body("ssh-key-id: 99"), jq: true, want: "refused", reason: "alice has no SSH key with id 99"},
		{name: "ecdsa", body: body("ssh-key-id: 13"), jq: true, want: "refused", reason: "is ecdsa-sha2-nistp256, not ssh-ed25519 or ssh-rsa"},
		{name: "ed25519", body: body("free text", "ssh-key-id: 11", "more"), jq: true, want: "ok"},
		{name: "rsa admin", body: body("ssh-key-id: 12"), permission: "admin", jq: true, want: "ok"},
	}
	for _, c := range cases {
		if c.jq && hasJQ != nil {
			t.Logf("%s: skipped, no jq", c.name)
			continue
		}
		dir := t.TempDir()
		bin := filepath.Join(dir, "bin")
		os.MkdirAll(bin, 0o755)
		os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755)
		def := func(v, d string) string {
			if v == "" {
				return d
			}
			return v
		}
		env := []string{
			"PATH=" + bin + ":" + os.Getenv("PATH"),
			"GH_LOG=" + filepath.Join(dir, "gh.log"),
			"GITHUB_OUTPUT=" + filepath.Join(dir, "output"),
			"RUNNER_TEMP=" + dir,
			"GH_TOKEN=job-token", "NIBFIN_TOKEN=nibfin-token",
			"REPO=o/r", "ISSUE=7", "LOGIN=alice",
			"TITLE=" + def(c.title, RunnerRequestTitle),
			"BODY=" + def(c.body, body("ssh-key-id: 11")),
			"USER_TYPE=" + def(c.userType, "User"),
			"FAKE_STATE=" + def(c.state, "open"),
			"FAKE_PERMISSION=" + def(c.permission, "write"),
			"FAKE_KEYS=" + keys,
		}
		out, err := runScript(t, script, env)
		if err != nil {
			t.Errorf("%s: %v\n%s", c.name, err, out)
			continue
		}
		output, _ := os.ReadFile(filepath.Join(dir, "output"))
		if !strings.Contains(string(output), "state="+c.want+"\n") {
			t.Errorf("%s: output %q, want state=%s\n%s", c.name, output, c.want, out)
		}
		refusal, _ := os.ReadFile(filepath.Join(dir, "nibfin-refusal.md"))
		if c.want == "refused" {
			if !strings.HasPrefix(string(refusal), RunnerReasonField+": ") || !strings.Contains(string(refusal), c.reason) {
				t.Errorf("%s: refusal %q, want %q", c.name, refusal, c.reason)
			}
		} else if len(refusal) != 0 {
			t.Errorf("%s: refusal %q", c.name, refusal)
		}
		key, _ := os.ReadFile(filepath.Join(dir, "nibfin-request.pub"))
		if c.want == "ok" {
			id := "11"
			if strings.Contains(c.body, "ssh-key-id: 12") {
				id = "12"
			}
			wantKey := map[string]string{"11": "ssh-ed25519 AAAAC3Nza alice@a\n", "12": "ssh-rsa AAAAB3Nza\n"}[id]
			if string(key) != wantKey || !strings.Contains(string(output), "key-id="+id+"\n") {
				t.Errorf("%s: key %q, output %q", c.name, key, output)
			}
		} else if len(key) != 0 {
			t.Errorf("%s: key written %q", c.name, key)
		}
		// The permission read uses the Nibfin token; every other call the job's.
		log, _ := os.ReadFile(filepath.Join(dir, "gh.log"))
		for _, l := range strings.Split(strings.TrimSpace(string(log)), "\n") {
			tok, call, _ := strings.Cut(l, "|")
			want := "job-token"
			if strings.Contains(call, "/permission") {
				want = "nibfin-token"
			}
			if tok != want {
				t.Errorf("%s: gh %s used %s", c.name, call, tok)
			}
		}
	}
}

// fakeAge stands in for age: it records its arguments and stdin and writes an
// armored stand-in ciphertext.
const fakeAge = `#!/bin/bash
printf '%s\n' "$@" >"$GH_DIR/age.args"
cat >"$GH_DIR/age.stdin"
printf '%s\n%s\n%s\n' '-----BEGIN AGE ENCRYPTED FILE-----' 'Y2lwaGVy' '-----END AGE ENCRYPTED FILE-----'
`

// fakeBin writes the fake gh and age into a new directory and returns it and
// the base environment of a step.
func fakeBin(t *testing.T) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755)
	os.WriteFile(filepath.Join(bin, "age"), []byte(fakeAge), 0o755)
	return dir, []string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"GH_LOG=" + filepath.Join(dir, "gh.log"), "GH_DIR=" + dir,
		"GITHUB_OUTPUT=" + filepath.Join(dir, "output"), "RUNNER_TEMP=" + dir,
		"GH_TOKEN=job-token", "REPO=o/r", "ISSUE=7", "LOGIN=alice",
	}
}

// TestRunnerRequestToken runs the token step: the token is masked first,
// reaches age only on stdin and appears in no file or output.
func TestRunnerRequestToken(t *testing.T) {
	needBash(t, "jq")
	d, _ := requestWorkflow(t)
	script := step(t, d, "Create and encrypt the registration token").Run
	dir, env := fakeBin(t)
	const reg = "AABBREGTOKEN0123"
	os.WriteFile(filepath.Join(dir, "nibfin-request.pub"), []byte("ssh-ed25519 AAAAC3Nza alice@a\n"), 0o644)
	out, err := runScript(t, script, append(env, "NIBFIN_TOKEN=nibfin-token", "KEY_ID=11", "FAKE_REG_TOKEN="+reg))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if first, _, _ := strings.Cut(out, "\n"); first != "::add-mask::"+reg || strings.Count(out, reg) != 1 {
		t.Errorf("log %q: the token is not masked before anything else", out)
	}
	if stdin, _ := os.ReadFile(filepath.Join(dir, "age.stdin")); string(stdin) != reg {
		t.Errorf("age stdin %q", stdin)
	}
	if args, _ := os.ReadFile(filepath.Join(dir, "age.args")); string(args) != "-a\n-R\n"+filepath.Join(dir, "nibfin-request.pub")+"\n" {
		t.Errorf("age args %q", args)
	}
	answer, _ := os.ReadFile(filepath.Join(dir, "nibfin-answer.md"))
	want := RunnerRegistrationMarker + "\n" +
		RunnerRequesterField + ": alice\n" +
		RunnerKeyIDField + ": 11\n" +
		RunnerExpiresField + ": 2026-11-25T13:00:00Z\n"
	block := "```text\n" + RunnerTokenBegin + "\nY2lwaGVy\n" + RunnerTokenEnd + "\n```\n"
	if !strings.HasPrefix(string(answer), want) || !strings.HasSuffix(string(answer), block) {
		t.Errorf("answer:\n%s", answer)
	}
	// Only the stand-in for age saw the token.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || e.Name() == "age.stdin" {
			continue
		}
		if b, _ := os.ReadFile(filepath.Join(dir, e.Name())); strings.Contains(string(b), reg) {
			t.Errorf("the token is in %s", e.Name())
		}
	}
	if log, _ := os.ReadFile(filepath.Join(dir, "gh.log")); strings.TrimSpace(string(log)) != "nibfin-token|api -X POST repos/o/r/actions/runners/registration-token" {
		t.Errorf("gh calls %q", log)
	}
}

// TestRunnerRequestAnswer runs the answer step: one comment per marker, a
// rerun updates the workflow's own comment, and a refusal closes the Issue.
func TestRunnerRequestAnswer(t *testing.T) {
	needBash(t, "jq")
	d, _ := requestWorkflow(t)
	script := step(t, d, "Answer the request").Run
	const ours = `{"id":5,"user":{"login":"github-actions[bot]"},"body":"`
	cases := []struct {
		name, status, state, comments string
		patch, closed                 bool
		marker, contains              string
	}{
		{name: "registration", status: "success", state: "ok", comments: `[]`, marker: RunnerRegistrationMarker, contains: "Y2lwaGVy"},
		{name: "registration again", status: "success", state: "ok", patch: true, marker: RunnerRegistrationMarker,
			comments: `[{"id":4,"user":{"login":"alice"},"body":"` + RunnerRegistrationMarker + ` fake"}]` + "\n" + `[` + ours + RunnerRegistrationMarker + `\nold"}]`},
		{name: "refusal", status: "success", state: "refused", comments: `[]`, closed: true, marker: RunnerRefusedMarker, contains: "reason: alice has read permission"},
		{name: "refusal again", status: "success", state: "refused", patch: true, closed: true, marker: RunnerRefusedMarker,
			comments: `[` + ours + RunnerRefusedMarker + `\nreason: old"}]`},
		{name: "failure", status: "failure", state: "", comments: `[` + ours + RunnerRegistrationMarker + `\nold"}]`, closed: true, marker: RunnerRefusedMarker,
			contains: "reason: the request workflow failed (https://github.com/o/r/actions/runs/1)"},
	}
	for _, c := range cases {
		dir, env := fakeBin(t)
		os.WriteFile(filepath.Join(dir, "nibfin-answer.md"), []byte(RunnerRegistrationMarker+"\nrequester: alice\n\n```text\nY2lwaGVy\n```\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "nibfin-refusal.md"), []byte("reason: alice has read permission on o/r, not write or admin\n\nAsk.\n"), 0o644)
		out, err := runScript(t, script, append(env, "JOB_STATUS="+c.status, "STATE="+c.state,
			"RUN_URL=https://github.com/o/r/actions/runs/1", "FAKE_COMMENTS="+c.comments))
		if err != nil {
			t.Errorf("%s: %v\n%s", c.name, err, out)
			continue
		}
		file := "post.json"
		if c.patch {
			file = "patch-5.json"
		}
		raw, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Errorf("%s: no %s: %v\n%s", c.name, file, err, out)
			continue
		}
		var payload struct {
			Body string `yaml:"body"`
		}
		if err := yaml.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(payload.Body, "\n"); first != c.marker || !strings.Contains(payload.Body, c.contains) {
			t.Errorf("%s: body %q", c.name, payload.Body)
		}
		entries, _ := filepath.Glob(filepath.Join(dir, "p*.json"))
		if len(entries) != 1 {
			t.Errorf("%s: comment writes %q", c.name, entries)
		}
		if _, err := os.Stat(filepath.Join(dir, "closed")); (err == nil) != c.closed {
			t.Errorf("%s: closed %v, want %v", c.name, err == nil, c.closed)
		}
	}
}

// TestRunnerRequestAnswerLostReply checks that a comment whose reply was
// lost is updated on the retry, not posted twice.
func TestRunnerRequestAnswerLostReply(t *testing.T) {
	needBash(t, "jq")
	d, _ := requestWorkflow(t)
	script := step(t, d, "Answer the request").Run
	dir, env := fakeBin(t)
	os.WriteFile(filepath.Join(dir, "nibfin-answer.md"), []byte(RunnerRegistrationMarker+"\nrequester: alice\n"), 0o644)
	out, err := runScript(t, script, append(env, "JOB_STATUS=success", "STATE=ok", "FAKE_COMMENTS=[]", "FAKE_POST_LOST=1"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "p*.json"))
	if len(entries) != 2 || !strings.HasSuffix(entries[0], "patch-6.json") || !strings.HasSuffix(entries[1], "post.json") {
		t.Errorf("comment writes %q", entries)
	}
	if !strings.Contains(out, "retry 1 of 3") || !strings.Contains(out, "sleep 1") {
		t.Errorf("output %q", out)
	}
}

// TestGHRetry runs the gh_retry helper of the templates: any failure is
// retried 3 times with 1, 2 and 4 seconds between, the output of a failed
// attempt is dropped, and no body field is logged.
func TestGHRetry(t *testing.T) {
	needBash(t)
	d, _ := requestWorkflow(t)
	run := step(t, d, "Create and encrypt the registration token").Run
	start := strings.Index(run, "gh_retry() {")
	end := strings.Index(run[start:], "\n}\n") + start + len("\n}\n")
	helper := run[start:end]
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	// The fake fails until its counter reaches FAKE_OK, printing an error body.
	os.WriteFile(filepath.Join(bin, "gh"), []byte(`#!/bin/bash
n=$(( $(cat "$COUNT" 2>/dev/null || echo 0) + 1 ))
echo "$n" >"$COUNT"
if [ "$n" -lt "$FAKE_OK" ]; then echo '{"message":"Bad Gateway"}'; exit 1; fi
echo "answer $n"
`), 0o755)
	for _, c := range []struct {
		ok    string
		fail  bool
		calls string
		out   string
	}{{"4", false, "4", "answer 4"}, {"9", true, "4", ""}, {"1", false, "1", "answer 1"}} {
		count := filepath.Join(dir, "count-"+c.ok)
		script := helper + "\nout=$(gh_retry api -X POST repos/o/r/keys -f key=SECRET)\necho \"got [$out]\"\n"
		out, err := runScript(t, script, []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "COUNT=" + count, "FAKE_OK=" + c.ok})
		if (err != nil) != c.fail {
			t.Errorf("FAKE_OK=%s: err %v\n%s", c.ok, err, out)
		}
		if b, _ := os.ReadFile(count); strings.TrimSpace(string(b)) != c.calls {
			t.Errorf("FAKE_OK=%s: %s calls", c.ok, b)
		}
		if !c.fail && !strings.Contains(out, "got ["+c.out+"]") {
			t.Errorf("FAKE_OK=%s: output %q", c.ok, out)
		}
		if strings.Contains(out, "SECRET") {
			t.Errorf("FAKE_OK=%s: a body field is logged: %q", c.ok, out)
		}
	}
	out, _ := runScript(t, helper+"\ngh_retry api -X POST repos/o/r/keys -f key=SECRET\n", []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "COUNT=" + filepath.Join(dir, "c2"), "FAKE_OK=9"})
	for _, want := range []string{"nibfin: GitHub API api repos/o/r/keys failed; retry 1 of 3", "sleep 1", "sleep 2", "sleep 4", "retry 3 of 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q has no %q", out, want)
		}
	}
}

// ed25519Blob is the base64 wire format of an ssh-ed25519 public key with
// the key bytes key (32 for a real key).
func ed25519Blob(algo string, key []byte) string {
	var b []byte
	put := func(p []byte) {
		b = append(b, byte(len(p)>>24), byte(len(p)>>16), byte(len(p)>>8), byte(len(p)))
		b = append(b, p...)
	}
	put([]byte(algo))
	put(key)
	return base64.StdEncoding.EncodeToString(b)
}

// TestRunnerRequestDeployKeyOrder checks that the deploy key step runs only
// for a request with a deploy key, before the token is created.
func TestRunnerRequestDeployKeyOrder(t *testing.T) {
	d, _ := requestWorkflow(t)
	var names []string
	for _, s := range d.Jobs["request"].Steps {
		names = append(names, s.Name)
	}
	want := []string{"Check the request", "Install age", "Add the deploy key", "Create and encrypt the registration token", "Answer the request"}
	if !slices.Equal(names, want) {
		t.Errorf("steps %q, want %q", names, want)
	}
	add := step(t, d, "Add the deploy key")
	if add.If != "steps.check.outputs.state == 'ok' && steps.check.outputs.deploy-key == 'yes'" {
		t.Errorf("deploy key step if %q", add.If)
	}
	for _, want := range []string{
		`GH_TOKEN="$NIBFIN_TOKEN" gh api -X POST "repos/$REPO/keys"`,
		`title="` + RunnerDeployKeyTitlePrefix + `$LOGIN"`,
		`-F read_only=true`,
	} {
		if !strings.Contains(add.Run, want) {
			t.Errorf("the deploy key step has no %q", want)
		}
	}
	check := step(t, d, "Check the request")
	if !strings.Contains(check.Run, `'^`+RunnerDeployKeyField+`:'`) {
		t.Error("the check step does not count the deploy-key lines")
	}
}

// TestRunnerRequestDeployKeyCheck runs the check step with deploy-key lines.
func TestRunnerRequestDeployKeyCheck(t *testing.T) {
	needBash(t, "grep", "sed", "jq", "base64", "od", "tr")
	d, _ := requestWorkflow(t)
	script := step(t, d, "Check the request").Run
	good := ed25519Blob("ssh-ed25519", []byte(strings.Repeat("k", 32)))
	keys := `[{"id":11,"key":"ssh-ed25519 AAAAC3Nza alice@a"}]`
	cases := []struct {
		name  string
		lines []string
		want  string // "no", "yes" or the refusal reason
	}{
		{name: "none", want: "no"},
		{name: "valid", lines: []string{RunnerDeployKeyField + ": ssh-ed25519 " + good}, want: "yes"},
		{name: "two", lines: []string{RunnerDeployKeyField + ": ssh-ed25519 " + good, RunnerDeployKeyField + ": ssh-ed25519 " + good},
			want: "the body has 2 deploy-key lines, want at most one"},
		{name: "rsa", lines: []string{RunnerDeployKeyField + ": ssh-rsa " + ed25519Blob("ssh-rsa", []byte(strings.Repeat("k", 32)))},
			want: "the deploy-key line is not an ssh-ed25519 public key"},
		{name: "rsa blob", lines: []string{RunnerDeployKeyField + ": ssh-ed25519 " + ed25519Blob("ssh-rsa", []byte(strings.Repeat("k", 36)))},
			want: "the deploy-key line is not an ssh-ed25519 public key"},
		{name: "garbage", lines: []string{RunnerDeployKeyField + ": ssh-ed25519 AAA*;$(id)"},
			want: "the deploy-key line is not an ssh-ed25519 public key"},
		{name: "bad base64", lines: []string{RunnerDeployKeyField + ": ssh-ed25519 A"},
			want: "the deploy-key line is not an ssh-ed25519 public key"},
		{name: "short", lines: []string{RunnerDeployKeyField + ": ssh-ed25519 " + ed25519Blob("ssh-ed25519", []byte(strings.Repeat("k", 31)))},
			want: "the deploy-key line is not an ssh-ed25519 public key"},
		{name: "long", lines: []string{RunnerDeployKeyField + ": ssh-ed25519 " + ed25519Blob("ssh-ed25519", []byte(strings.Repeat("k", 33)))},
			want: "the deploy-key line is not an ssh-ed25519 public key"},
		{name: "comment", lines: []string{RunnerDeployKeyField + ": ssh-ed25519 " + good + " me@host"},
			want: "the deploy-key line is not an ssh-ed25519 public key"},
	}
	for _, c := range cases {
		dir, env := fakeBin(t)
		body := strings.Join(append([]string{RunnerRequestMarker, "ssh-key-id: 11"}, c.lines...), "\r\n")
		env = append(env, "NIBFIN_TOKEN=nibfin-token", "TITLE="+RunnerRequestTitle, "BODY="+body, "USER_TYPE=User",
			"FAKE_STATE=open", "FAKE_PERMISSION=write", "FAKE_KEYS="+keys)
		out, err := runScript(t, script, env)
		if err != nil {
			t.Errorf("%s: %v\n%s", c.name, err, out)
			continue
		}
		output, _ := os.ReadFile(filepath.Join(dir, "output"))
		pub, _ := os.ReadFile(filepath.Join(dir, "nibfin-deploy-key.pub"))
		refusal, _ := os.ReadFile(filepath.Join(dir, "nibfin-refusal.md"))
		switch c.want {
		case "no", "yes":
			if !strings.Contains(string(output), "state=ok\n") || !strings.Contains(string(output), "deploy-key="+c.want+"\n") {
				t.Errorf("%s: output %q\n%s", c.name, output, out)
			}
			if wantPub := map[string]string{"no": "", "yes": "ssh-ed25519 " + good + "\n"}[c.want]; string(pub) != wantPub {
				t.Errorf("%s: deploy key file %q", c.name, pub)
			}
		default:
			if !strings.Contains(string(output), "state=refused\n") || !strings.Contains(string(refusal), c.want) {
				t.Errorf("%s: output %q, refusal %q", c.name, output, refusal)
			}
			if len(pub) != 0 {
				t.Errorf("%s: deploy key file written %q", c.name, pub)
			}
		}
		// The body's key never reaches the step outputs.
		if strings.Contains(string(output), "ssh-ed25519") || strings.Contains(string(output), good) {
			t.Errorf("%s: output carries the key %q", c.name, output)
		}
	}
}

// TestRunnerRequestAddDeployKey runs the deploy key step: GitHub adds the
// key read-only, or it is already a deploy key of the repository; any
// other failure fails the step with GitHub's error.
func TestRunnerRequestAddDeployKey(t *testing.T) {
	needBash(t, "jq", "sed")
	d, _ := requestWorkflow(t)
	script := step(t, d, "Add the deploy key").Run
	blob := ed25519Blob("ssh-ed25519", []byte(strings.Repeat("k", 32)))
	other := ed25519Blob("ssh-ed25519", []byte(strings.Repeat("o", 32)))
	cases := []struct {
		name, post, list string
		listFail         bool
		ok               bool
		log              string
	}{
		{name: "added", post: "ok", ok: true, log: "Added the read-only deploy key nibfin-runner-alice to o/r (id 99)"},
		{name: "present", list: `[{"id":5,"key":"ssh-ed25519 ` + other + `","read_only":true}]` + "\n" +
			`[{"id":6,"key":"ssh-ed25519 ` + blob + `","title":"x","read_only":true}]`, ok: true,
			log: "already a deploy key of o/r (id 6, read_only true)"},
		{name: "not present", list: `[{"id":5,"key":"ssh-ed25519 ` + other + `","read_only":true}]`, log: "Validation Failed (HTTP 422)"},
		{name: "list fails", listFail: true, log: "Validation Failed (HTTP 422)"},
	}
	for _, c := range cases {
		dir, env := fakeBin(t)
		os.WriteFile(filepath.Join(dir, "nibfin-deploy-key.pub"), []byte("ssh-ed25519 "+blob+"\n"), 0o644)
		env = append(env, "NIBFIN_TOKEN=nibfin-token", "FAKE_KEY_POST="+c.post, "FAKE_DEPLOY_KEYS="+c.list)
		if c.listFail {
			env = append(env, "FAKE_KEYS_LIST_FAIL=1")
		}
		out, err := runScript(t, script, env)
		if (err == nil) != c.ok || !strings.Contains(out, c.log) {
			t.Errorf("%s: %v, want ok %v, log %q:\n%s", c.name, err, c.ok, c.log, out)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "gh.log"))
		lines := strings.Split(strings.TrimSpace(string(log)), "\n")
		wantPost := "nibfin-token|api -X POST repos/o/r/keys -f title=nibfin-runner-alice -f key=ssh-ed25519 " + blob + " -F read_only=true --jq .id"
		if lines[0] != wantPost {
			t.Errorf("%s: POST %q, want %q", c.name, lines[0], wantPost)
		}
		for _, l := range lines {
			if !strings.HasPrefix(l, "nibfin-token|") {
				t.Errorf("%s: gh call without the Nibfin token: %q", c.name, l)
			}
		}
		// A refused add is read back, then sent again: up to 4 times each.
		wantCalls := 1
		switch {
		case c.post == "ok":
		case strings.Contains(c.list, blob):
			wantCalls = 2
		default:
			wantCalls = 8
		}
		if len(lines) != wantCalls {
			t.Errorf("%s: gh calls %q", c.name, lines)
		}
	}
}
