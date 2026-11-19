package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

const nativeWakeSecret = "NIBFIN_WAKE_TOKEN"

type nativeSecretMetadata struct {
	Name      string `json:"name"`
	UpdatedAt string `json:"updated_at"`
}

type nativeSetupRequest struct {
	Account     User
	Target      Repository
	Destination SharedExecution
}

type nativeSecretPublisher interface {
	PublishWakeSecret(string) error
}

// gh encrypts stdin with the repository public key. Its authentication is bound
// to the same captured account as all preceding provider reads. Nothing from
// stdout/stderr is surfaced, including on response loss.
func (g *selectedGitHub) PublishWakeSecret(repo string) error {
	path, err := toolPath("gh")
	if err != nil {
		return err
	}
	cmd := nativeSecretCommand(g.ctx, path, repo, g.token)
	var diagnostic boundedBuffer
	cmd.Stderr = &diagnostic
	if cmd.Run() != nil {
		if nativeSecretPermissionRefused(diagnostic.String()) {
			return errors.New("NATIVE_SECRET_PERMISSION_REQUIRED")
		}
		return errors.New("NATIVE_SECRET_WRITE_UNCONFIRMED")
	}
	return nil
}

func nativeSecretPermissionRefused(diagnostic string) bool {
	return strings.Contains(diagnostic, "HTTP 403:") ||
		strings.Contains(diagnostic, "HTTP 401:") ||
		strings.Contains(diagnostic, "(HTTP 403)") ||
		strings.Contains(diagnostic, "(HTTP 401)")
}

func nativeSecretCommand(ctx context.Context, path, repo string, token []byte) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, "secret", "set", nativeWakeSecret,
		"--repo", repo, "--app", "actions")
	cmd.Env = append(selectedLoginEnvironment(), "GH_HOST=github.com",
		"GH_TOKEN="+string(token))
	cmd.Stdin = bytes.NewReader(token)
	return cmd
}

func verifyNativeSetupIdentity(api API, request nativeSetupRequest) error {
	for _, target := range []Repository{request.Target, {
		ID: request.Destination.RepositoryID, FullName: request.Destination.Repository,
	}} {
		repo, account, err := repository(api, target.FullName, true)
		if err != nil {
			return err
		}
		if repo.ID != target.ID || account.ID != request.Account.ID ||
			!strings.EqualFold(account.Login, request.Account.Login) {
			return errors.New("NATIVE_SETUP_IDENTITY_CHANGED")
		}
		// Check the effective Actions surface with this credential, rather than
		// inferring access from OAuth headers or repository ownership alone.
		var permissions struct {
			Enabled bool `json:"enabled"`
		}
		err = api.Call("GET", "repos/"+repo.FullName+"/actions/permissions",
			nil, &permissions)
		if err != nil || !permissions.Enabled {
			return errors.New("NATIVE_ACTIONS_PERMISSION_REQUIRED")
		}
	}
	return nil
}

func prepareNativeSetup(api API, publisher nativeSecretPublisher,
	request nativeSetupRequest, observation *nativeCredentialObservation,
	save func() error) error {
	if err := verifyNativeSetupIdentity(api, request); err != nil {
		return err
	}
	endpoint := "repos/" + request.Target.FullName + "/actions/secrets/" + nativeWakeSecret
	var metadata nativeSecretMetadata
	err := api.Call("GET", endpoint, nil, &metadata)
	if err == nil {
		if observation.State == "confirmed" && metadata.Name == nativeWakeSecret &&
			metadata.UpdatedAt != "" && metadata.UpdatedAt == observation.UpdatedAt {
			_, err := prepareSharedWakeDestination(api, request.Target.FullName,
				request.Destination)
			return err
		}
		if observation.State == "attempted" {
			return &CommandError{Code: "NATIVE_SECRET_EFFECT_UNKNOWN", Exit: 2}
		}
		return errors.New("NATIVE_SECRET_CONFLICT")
	}
	if err.Error() != "NOT_FOUND" {
		return err
	}
	if err := requireInactiveSharedWake(api, request.Target.FullName); err != nil {
		return err
	}
	if _, err := prepareSharedWakeDestination(api, request.Target.FullName,
		request.Destination); err != nil {
		return err
	}
	if publisher == nil {
		return errors.New("NATIVE_SECRET_PUBLISHER_UNAVAILABLE")
	}
	observation.State, observation.UpdatedAt = "attempted", ""
	if err := save(); err != nil {
		return err
	}
	if err := publisher.PublishWakeSecret(request.Target.FullName); err != nil {
		if err.Error() == "NATIVE_SECRET_PERMISSION_REQUIRED" {
			observation.State = ""
			if err := save(); err != nil {
				return err
			}
			return err
		}
		return &CommandError{Code: "NATIVE_SECRET_EFFECT_UNKNOWN", Exit: 2}
	}
	if err := api.Call("GET", endpoint, nil, &metadata); err != nil ||
		metadata.Name != nativeWakeSecret || metadata.UpdatedAt == "" {
		return &CommandError{Code: "NATIVE_SECRET_EFFECT_UNKNOWN", Exit: 2}
	}
	observation.State, observation.UpdatedAt = "confirmed", metadata.UpdatedAt
	return save()
}

func nativeSetupFromPlan(plan InitPlan) nativeSetupRequest {
	return nativeSetupRequest{plan.Member, plan.Repository, *plan.Execution}
}

// Read only the existing private intent; an absent record is normal unfinished
// work. It never authorizes conversion of a relay or peer installation.
func nativeManagedIntent(repo Repository) (bool, error) {
	path, err := managedIntentPath(repo)
	if err != nil {
		return false, err
	}
	file, err := privateInstallFile(path, os.O_RDONLY)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()
	var intent managedInstallIntent
	decoder := json.NewDecoder(io.LimitReader(file, 2*1024*1024+1))
	if decoder.Decode(&intent) != nil || decoder.Decode(new(any)) != io.EOF {
		return false, errors.New("MANAGED_INSTALL_INTENT_CHANGED")
	}
	return intent.Plan.NativeManaged, nil
}

// Loss of local bookkeeping must still enter provider observation. An existing
// secret cannot prove its contents. Ready adopted native installations recover
// through readiness readback; other installations retain setup conflict handling.
func observedNativeManagedRoute(api API, host sharedHost) (bool, error) {
	plan, err := planInitRecipe(api, host.Repository, host.Repository, "issue")
	if err != nil {
		if strings.HasPrefix(err.Error(), "EXISTING_FILE_CONFLICT:") {
			return false, nil
		}
		return false, err
	}
	if len(plan.Files) != 0 || plan.Repository.ID != host.RepositoryID ||
		plan.Member.ID != host.MemberID {
		return false, nil
	}
	var secret nativeSecretMetadata
	err = api.Call("GET", "repos/"+host.Repository+"/actions/secrets/"+nativeWakeSecret,
		nil, &secret)
	if err != nil {
		if err.Error() == "NOT_FOUND" {
			return false, nil
		}
		return false, err
	}
	return secret.Name == nativeWakeSecret, nil
}
