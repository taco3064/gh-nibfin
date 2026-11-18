package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type dashboardObserver struct {
	process Process
	subject snapshot.Subject
	resolve func() (string, string, error)
}

func (o dashboardObserver) run(program string, args []string,
	input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, native := o.process.(NativeProcess); native && len(args) == 2 &&
		args[1] == "observe-local-work" {
		return runNativeProcess(ctx, program, args, input, true)
	}
	if process, ok := o.process.(interface {
		RunContext(context.Context, string, []string, []byte) ([]byte, error)
	}); ok {
		return process.RunContext(ctx, program, args, input)
	}
	// Injected Process implementations share the existing bounded-process contract.
	return o.process.Run(program, args, input)
}

func (o dashboardObserver) Observe() snapshot.LocalObservation {
	unavailable := snapshot.UnavailableObservation
	if o.process == nil {
		return unavailable("UNAVAILABLE")
	}
	resolve := o.resolve
	if resolve == nil {
		resolve = overviewCore
	}
	node, entry, err := resolve()
	if err != nil {
		if err.Error() == "UNSUPPORTED_LOCAL_OBSERVATION_PLATFORM" {
			return unavailable("UNSUPPORTED_PLATFORM")
		}
		return unavailable("INSTALLED_CORE_UNAVAILABLE")
	}
	// Help is a capability read, never an install/upgrade or administrator call.
	help, err := o.run(node, []string{entry, "help"}, nil)
	var commands struct {
		Commands []string `json:"commands"`
	}
	if err != nil || len(help) > 65536 || json.Unmarshal(help, &commands) != nil {
		return unavailable("UNAVAILABLE")
	}
	if !overviewCommand(commands.Commands, "observe-local-work") {
		return unavailable("UNSUPPORTED_CORE")
	}
	token, err := o.run("gh", []string{"auth", "token", "--hostname", "github.com"}, nil)
	if err != nil || len(token) > 16385 {
		return unavailable("CREDENTIAL_UNAVAILABLE")
	}
	credential := strings.TrimSpace(string(token))
	if len(credential) > 16384 || credential == "" ||
		strings.ContainsAny(credential, " \t\r\n\x00") {
		return unavailable("CREDENTIAL_UNAVAILABLE")
	}
	input, _ := json.Marshal(struct {
		Token string `json:"token"`
	}{credential})
	data, err := o.run(node, []string{entry, "observe-local-work"}, input)
	if err != nil {
		// Unclassified failures disclose neither process output nor diagnostics.
		return unavailable("OBSERVATION_UNAVAILABLE")
	}
	value := snapshot.DecodeLocalObservation(data, o.subject)
	if value.Status != "observed" {
		return value
	}
	if overviewCommand(commands.Commands, "inspect-shared-host") {
		data, err := o.run(node, []string{entry, "inspect-shared-host"}, nil)
		var host *sharedHost
		if err == nil && len(data) <= 256*1024 {
			host, err = decodeSharedHost(data)
		}
		if err == nil && host != nil && host.MemberID == o.subject.ID &&
			len(host.AdoptedRepositoryIDs) <= 32 {
			value.AdoptedIDs = host.AdoptedRepositoryIDs
		} else {
			value.Completeness = "partial"
		}
	}
	return value
}

func dashboardSubject(api API) (snapshot.Subject, error) {
	var user User
	if api == nil || api.Call("GET", "user", nil, &user) != nil || user.ID <= 0 ||
		user.Type != "User" || !dashboardLogin.MatchString(user.Login) {
		return snapshot.Subject{}, errors.New("DASHBOARD_AUTHENTICATED_READ_REQUIRED")
	}
	return snapshot.Subject{ID: user.ID, Login: user.Login}, nil
}

func overviewCommand(commands []string, name string) bool {
	for _, command := range commands {
		if command == name || strings.HasPrefix(command, name+" (") {
			return true
		}
	}
	return false
}

// Detail reads in overview mode keep the authenticated subject and native read
// permission check while leaving the ordinary repository Snapshot API unchanged.
type dashboardOverviewRepository struct {
	api     API
	subject snapshot.Subject
}

func (p dashboardOverviewRepository) Collect(name string) snapshot.Snapshot {
	current, err := dashboardSubject(p.api)
	if err != nil || current.ID != p.subject.ID ||
		!strings.EqualFold(current.Login, p.subject.Login) ||
		dashboardRepositoryRead(p.api, name) != nil {
		return (snapshot.Provider{}).Collect(name)
	}
	return (snapshot.Provider{API: p.api}).Collect(name)
}
