// Package runnerreq holds the runner request contract that
// nibfin-runner-request.yml (internal/templates) and gh nibfin runner add
// (internal/op) share. It imports nothing, so both packages can use it.
//
// The runner request contract. A collaborator of the execution repository asks
// for a runner registration token by opening an Issue there;
// nibfin-runner-request.yml answers it. gh nibfin runner add on the
// collaborator's host opens the request and reads the answer, so both sides
// use these values.
//
// The request is an Issue whose title is RunnerRequestTitle and whose body has
// the line RunnerRequestMarker and exactly one line
//
//	ssh-key-id: <id>
//
// where <id> is the id of one of the author's SSH keys in
// GET /users/{login}/keys (ssh-ed25519 or ssh-rsa). Other body text is
// ignored. The requester is always the Issue's author, never a body field.
//
// On a private execution repository the body also has at most one line
//
//	deploy-key: ssh-ed25519 <base64>
//
// the public key of the requesting host's dedicated deploy key. Before it
// answers, the workflow adds that key to the repository as a read-only
// deploy key titled RunnerDeployKeyTitlePrefix + <author login>; a key that
// is already a deploy key of the repository is accepted. Several such lines,
// or a line that is not an ssh-ed25519 public key, is a refusal; a failure to
// add the key is a failure answer, and the Issue is closed.
//
// The answer is one comment by the workflow's GITHUB_TOKEN identity. A
// registration starts with the line RunnerRegistrationMarker, then the lines
// "requester: <login>", "ssh-key-id: <id>" and "expires-at: <time>", and holds
// the registration token encrypted with age to that SSH key, armored, in a
// fenced text block between RunnerTokenBegin and RunnerTokenEnd. A refusal
// starts with the line RunnerRefusedMarker, then the line "reason: <text>";
// the workflow then closes the Issue as not planned.
package runnerreq

const (
	// RunnerRequestFile is the workflow that answers requests, in the
	// execution set.
	RunnerRequestFile = "nibfin-runner-request.yml"
	// RunnerRequestTitle is the exact title of a request Issue.
	RunnerRequestTitle = "Nibfin runner request"
	// RunnerRequestMarker is the body line that makes an Issue a request.
	RunnerRequestMarker = "<!-- nibfin:runner-request -->"
	// RunnerKeyIDField names the requested SSH key in the request body and
	// in the registration comment.
	RunnerKeyIDField = "ssh-key-id"
	// RunnerDeployKeyField names the optional deploy key in the request
	// body.
	RunnerDeployKeyField = "deploy-key"
	// RunnerDeployKeyTitlePrefix starts the title of the deploy key the
	// workflow adds; the author's login follows it.
	RunnerDeployKeyTitlePrefix = "nibfin-runner-"
	// RunnerRegistrationMarker is the first line of a registration comment.
	RunnerRegistrationMarker = "<!-- nibfin:runner-registration -->"
	// RunnerRefusedMarker is the first line of a refusal comment.
	RunnerRefusedMarker = "<!-- nibfin:runner-request-refused -->"
	// RunnerRequesterField names the requester's login in a registration.
	RunnerRequesterField = "requester"
	// RunnerExpiresField names the token's expiry time in a registration.
	RunnerExpiresField = "expires-at"
	// RunnerReasonField names the reason in a refusal.
	RunnerReasonField = "reason"
	// RunnerTokenBegin and RunnerTokenEnd enclose the encrypted token.
	RunnerTokenBegin = "-----BEGIN AGE ENCRYPTED FILE-----"
	RunnerTokenEnd   = "-----END AGE ENCRYPTED FILE-----"
)
