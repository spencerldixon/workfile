package workfile

// CI rules use GitHub's combined check/status result for the PR's current commit.
func passingCI(e Expression) bool {
	return e.Fact == "checks" && e.Op == "==" && e.Value == "success"
}

func ciEvidence(value any) string {
	switch value {
	case "success":
		return "CI is passing on the current commit"
	case "pending":
		return "CI is still running on the current commit"
	case "failure":
		return "CI failed on the current commit"
	case "none":
		return "No CI checks reported on the current commit"
	default:
		return "CI status is unavailable for the current commit"
	}
}

func ciNextAction(value any) string {
	switch value {
	case "pending":
		return "Wait for CI to finish successfully."
	case "failure":
		return "Fix the failing CI checks, then rerun them."
	case "none":
		return "Run CI for this PR's current commit."
	default:
		return "Get passing CI for this PR's current commit."
	}
}
