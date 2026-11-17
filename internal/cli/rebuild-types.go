package cli

// Retirement and activation are Core facts. Durable CLI fields only bind retries.
type rebuildRetirement struct {
	ID           string                  `json:"id"`
	Settled      bool                    `json:"settled"`
	Phase        string                  `json:"phase"`
	Pool         *sharedHost             `json:"pool"`
	Repositories []preparationRepository `json:"repositories"`
	Replacement  *sharedHost             `json:"replacement"`
	Release      string                  `json:"release"`
}

type preparationRepository struct {
	Repository string `json:"repository"`
	ID         int64  `json:"repositoryId"`
}

type rebuildSnapshot struct {
	Draining     bool                    `json:"draining"`
	Host         *sharedHost             `json:"host"`
	Repositories []preparationRepository `json:"repositories"`
	Retirement   *rebuildRetirement      `json:"retirement"`
}

type rebuildIntent struct {
	Version      int                     `json:"schemaVersion"`
	Old          sharedHost              `json:"old"`
	Agent        agentBinding            `json:"agent"`
	Repositories []preparationRepository `json:"repositories"`
	RetirementID string                  `json:"retirementId,omitempty"`
	Fresh        []sharedRegistration    `json:"fresh,omitempty"`
	Paths        []string                `json:"paths,omitempty"`
}
