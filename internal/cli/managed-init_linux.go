package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
)

func managedInstallConsent(plan InitPlan, explicit bool) error {
	if explicit {
		return nil
	}
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return errors.New("MANAGED_INSTALL_REQUIRES_CONSENT_MANAGED_LOGIN")
	}
	defer terminal.Close()
	_, err = fmt.Fprintf(terminal,
		"Reuse github.com login %s for unattended Nibfin on this dedicated host? "+
			"Core will manage its Docker Engine and Controller credentials. [y/N] ",
		plan.Member.Login)
	if err != nil {
		return errors.New("MANAGED_INSTALL_CONSENT_UNAVAILABLE")
	}
	answer, err := bufio.NewReaderSize(terminal, 32).ReadSlice('\n')
	if err != nil || !strings.EqualFold(strings.TrimSpace(string(answer)), "y") {
		return errors.New("MANAGED_INSTALL_CONSENT_DECLINED")
	}
	return nil
}

func privateInstallFile(path string, flags int) (*os.File, error) {
	file, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err == nil {
		owner, ok := stat.Sys().(*syscall.Stat_t)
		if ok && stat.Mode().IsRegular() && stat.Mode().Perm() == 0600 &&
			owner.Uid == uint32(os.Getuid()) && owner.Nlink == 1 {
			return file, nil
		}
	}
	file.Close()
	return nil, errors.New("MANAGED_INSTALL_PRIVATE_FILE_REQUIRED")
}

func managedIntentPath(repo Repository) (string, error) {
	root, err := os.UserConfigDir()
	if err != nil || !filepath.IsAbs(root) || repo.ID <= 0 {
		return "", errors.New("MANAGED_INSTALL_STATE_UNAVAILABLE")
	}
	return filepath.Join(root, "nibfin", "install",
		fmt.Sprintf("%d.json", repo.ID)), nil
}

func managedIntentAvailable(repo Repository) bool {
	path, err := managedIntentPath(repo)
	if err != nil {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func installIntentLock(path string) (func(), error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return nil, errors.New("MANAGED_INSTALL_STATE_UNSAFE")
	}
	stat, err := os.Lstat(dir)
	if err != nil || !stat.IsDir() || stat.Mode().Perm() != 0700 ||
		stat.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return nil, errors.New("MANAGED_INSTALL_STATE_UNSAFE")
	}
	lock, err := privateInstallFile(path+".lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("MANAGED_INSTALL_IN_PROGRESS")
	}
	return func() { _ = lock.Close() }, nil
}

func saveManagedIntent(path string, x managedInstallIntent) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".nibfin-install-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := json.NewEncoder(temp).Encode(x); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func loadManagedIntent(path string, plan InitPlan) (managedInstallIntent, error) {
	intent := managedInstallIntent{Plan: plan, Pins: Pins()}
	file, err := privateInstallFile(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		if len(plan.Files) == 0 {
			return intent, errors.New("MANAGED_INSTALL_ORIGINAL_PR_INTENT_REQUIRED")
		}
		return intent, saveManagedIntent(path, intent)
	}
	if err != nil {
		return intent, err
	}
	defer file.Close()
	const limit = 2 * 1024 * 1024
	err = json.NewDecoder(io.LimitReader(file, limit)).Decode(&intent)
	if err != nil || intent.Plan.Repository.ID != plan.Repository.ID ||
		intent.Plan.Repository.FullName != plan.Repository.FullName ||
		intent.Plan.Repository.DefaultBranch != plan.Repository.DefaultBranch ||
		intent.Plan.Member.ID != plan.Member.ID ||
		!sameSharedExecution(intent.Plan.Execution, plan.Execution) ||
		!managedIntentRecipeCompatible(intent.Plan, plan) ||
		!reflect.DeepEqual(intent.Pins, Pins()) ||
		!commitPattern.MatchString(intent.Plan.Head) ||
		len(intent.Plan.Files) == 0 {
		return intent, errors.New("MANAGED_INSTALL_INTENT_CHANGED")
	}
	return intent, nil
}

type onboardingProcess struct {
	ctx     context.Context
	process NativeProcess
}

func (p onboardingProcess) Run(name string, args []string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 50*time.Minute)
	defer cancel()
	return p.process.RunContext(ctx, name, args, input)
}

func codexCredentialEnvelope(token []byte) ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.New("MANAGED_CODEX_DEDICATED_LOGIN_REQUIRED")
	}
	dir := filepath.Join(home, ".codex")
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return nil, errors.New("MANAGED_CODEX_DEDICATED_LOGIN_REQUIRED")
	}
	file, err := privateInstallFile(filepath.Join(dir, "auth.json"), os.O_RDONLY)
	if err != nil {
		return nil, errors.New("MANAGED_CODEX_DEDICATED_LOGIN_REQUIRED")
	}
	defer file.Close()
	auth, err := io.ReadAll(io.LimitReader(file, 65537))
	defer clear(auth)
	if err != nil || len(auth) > 65536 || !json.Valid(auth) {
		return nil, errors.New("MANAGED_CODEX_DEDICATED_LOGIN_REQUIRED")
	}
	var mode struct {
		AuthMode string `json:"auth_mode"`
	}
	if json.Unmarshal(auth, &mode) != nil || mode.AuthMode != "chatgpt" {
		return nil, errors.New("MANAGED_CODEX_DEDICATED_LOGIN_REQUIRED")
	}
	data, err := json.Marshal(struct {
		Version int             `json:"schemaVersion"`
		Token   string          `json:"githubToken"`
		Codex   json.RawMessage `json:"codex"`
	}{1, string(token), auth})
	if err != nil || len(data) > 65536 {
		clear(data)
		return nil, errors.New("MANAGED_CREDENTIAL_INPUT_INVALID")
	}
	return data, nil
}

func (a App) runManagedInit(plan InitPlan, consent bool, out io.Writer) error {
	if os.Getuid() == 0 {
		return errors.New("MANAGED_INSTALL_REQUIRES_INSTALLING_USER")
	}
	if err := managedInstallConsent(plan, consent); err != nil {
		return err
	}
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	api, err := selectedLogin(ctx, plan.Member)
	if err != nil {
		return err
	}
	defer clear(api.token)
	a.API = api
	a.Process = onboardingProcess{ctx: ctx, process: NativeProcess{}}
	return a.resumeManagedInit(ctx, plan, api.token, out, a.managedHostOperations())
}

// The production caller supplies only the selected native login and fixed host
// operations. In-package Go drivers can exercise the same orchestration.
func (a App) resumeManagedInit(ctx context.Context, plan InitPlan, token []byte,
	out io.Writer, host managedHostOperations) error {
	if err := a.requireHandoff(); err != nil {
		return err
	}
	api := a.API
	pin := Pins().Core
	if _, _, err := content(api, pin.Repository, managedHostEntry, pin.Commit); err != nil {
		return errors.New("MANAGED_INSTALL_CORE_SOURCE_REQUIRED")
	}
	current, err := planInitRecipe(api, plan.Repository.FullName,
		plan.Execution.Repository, initIssueRecipe(plan))
	if err != nil || current.Member.ID != plan.Member.ID ||
		current.Repository.ID != plan.Repository.ID {
		return errors.New("MANAGED_INSTALL_PLAN_REQUIRES_READBACK")
	}
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		return err
	}
	closeLock, err := installIntentLock(path)
	if err != nil {
		return err
	}
	defer closeLock()
	if len(current.Files) == 0 && !managedIntentAvailable(current.Repository) &&
		initIssueRecipe(current) == "issue-managed" {
		return a.finishVerifiedManagedInit(ctx, current, token, json.NewEncoder(out), host)
	}
	intent, err := loadManagedIntent(path, current)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	if initIssueRecipe(intent.Plan) == "issue" &&
		initIssueRecipe(current) == "issue-managed" && len(current.Files) == 0 {
		return a.finishVerifiedManagedInit(ctx, current, token, encoder, host)
	}
	wait, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := waitManagedInstall(wait, api, &intent, func() error {
		return saveManagedIntent(path, intent)
	}, func(url string) error {
		return encoder.Encode(InitResult{Status: "pending",
			Code: "MANAGED_INSTALL_PR_READ_BACK", PR: url})
	}); err != nil {
		return err
	}
	current, err = planInitRecipe(api, plan.Repository.FullName,
		plan.Execution.Repository, initIssueRecipe(plan))
	if err != nil || current.Member.ID != plan.Member.ID ||
		current.Repository.ID != plan.Repository.ID || len(current.Files) != 0 {
		return errors.New("MANAGED_INSTALL_PLAN_REQUIRES_READBACK")
	}
	return a.finishVerifiedManagedInit(ctx, current, token, encoder, host)
}

func (a App) finishVerifiedManagedInit(ctx context.Context, plan InitPlan, token []byte,
	encoder installResultEncoder, host managedHostOperations) error {
	if err := a.requireHandoff(); err != nil {
		return err
	}
	if err := ensureInitLabels(a.API, "repos/"+plan.Repository.FullName,
		plan.Labels); err != nil {
		return err
	}
	return a.finishManagedInit(ctx, plan, token, encoder, host)
}
