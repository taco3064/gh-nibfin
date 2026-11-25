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
