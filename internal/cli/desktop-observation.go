package cli

import "strings"

// Keep provider text out of diagnostics, even when it resembles a preflight report.
func sanitizedDesktopLinux(o linuxTargetObservation) *linuxTargetObservation {
	if len(o.Distribution) > 32 || o.Distribution == "" ||
		strings.Trim(o.Distribution, "abcdefghijklmnopqrstuvwxyz0123456789.-") != "" ||
		len(o.Version) > 32 || o.Version == "" ||
		strings.Trim(o.Version, "0123456789.-") != "" ||
		len(o.KernelArch) > 32 || !platformAlias.MatchString(o.KernelArch) ||
		(o.RuntimeArch != "amd64" && o.RuntimeArch != "arm64") ||
		(o.Architecture != "amd64" && o.Architecture != "arm64" &&
			o.Architecture != "") || o.Readiness != "unverified" {
		return nil
	}
	result := &linuxTargetObservation{Distribution: o.Distribution, Version: o.Version,
		KernelArch: o.KernelArch, RuntimeArch: o.RuntimeArch, Architecture: o.Architecture,
		ExecutableMachine: o.ExecutableMachine, Readiness: "unverified"}

	result.Reclamation = sanitizedReclamation(o.Reclamation)
	result.Checks = sanitizedDesktopChecks(o.Checks, linuxPrerequisiteChecks, true)
	optional := []string{"docker_endpoint", "dedicated_engine", "producer_platform",
		"docker.service", "nibfin-engine.service"}
	extra := sanitizedDesktopChecks(o.Checks, optional, false)
	result.Checks = append(result.Checks, extra...)
	return result
}

func sanitizedDesktopChecks(checks []capability, names []string,
	required bool) []capability {
	var result []capability
	for _, name := range names {
		status := "unverified"
		matches := 0
		for _, check := range checks {
			if check.Name == name {
				matches++
				if check.Status == "observed" || check.Status == "available" {
					status = check.Status
				}
			}
		}
		if matches == 0 && !required {
			continue
		}
		if matches != 1 {
			status = "unverified"
		}
		code := "GUEST_CAPABILITY_UNVERIFIED"
		if status != "unverified" {
			code = "GUEST_CAPABILITY_OBSERVED_ONLY"
		}
		result = append(result, capability{name, status, code})
	}
	return result
}
