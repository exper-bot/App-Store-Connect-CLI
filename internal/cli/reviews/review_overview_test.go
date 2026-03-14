package reviews

import (
	"testing"

	"github.com/rudrankriyam/App-Store-Connect-CLI/internal/validation"
)

func TestBuildReviewStatusResultMissingVersion(t *testing.T) {
	result := buildReviewStatusResult(reviewSnapshot{AppID: "123456789"})

	if result.ReviewState != "NO_VERSION" {
		t.Fatalf("expected NO_VERSION state, got %q", result.ReviewState)
	}
	if result.NextAction == "" {
		t.Fatal("expected next action for missing version")
	}
	if len(result.Blockers) != 1 {
		t.Fatalf("expected one blocker, got %d", len(result.Blockers))
	}
}

func TestBuildReviewDoctorResultAddsSyntheticUnresolvedIssuesBlocker(t *testing.T) {
	snapshot := reviewSnapshot{
		AppID: "123456789",
		Version: &reviewVersionContext{
			ID:       "ver-1",
			Version:  "1.2.3",
			Platform: "IOS",
			State:    "WAITING_FOR_REVIEW",
		},
		LatestSubmission: &reviewSubmissionContext{
			ID:    "review-sub-1",
			State: "UNRESOLVED_ISSUES",
		},
	}
	report := validation.Report{
		Summary: validation.Summary{Errors: 1, Blocking: 1},
		Checks: []validation.CheckResult{
			{
				ID:          "review.details.missing",
				Severity:    validation.SeverityError,
				Message:     "Review details are missing",
				Remediation: "Create review details.",
			},
		},
	}

	result := buildReviewDoctorResult(snapshot, report)

	if len(result.BlockingChecks) < 2 {
		t.Fatalf("expected synthetic blocker plus readiness blocker, got %d", len(result.BlockingChecks))
	}
	if result.BlockingChecks[0].ID != "review.details.missing" && result.BlockingChecks[0].ID != "review.submission.unresolved_issues" {
		t.Fatalf("expected known blocker ID, got %q", result.BlockingChecks[0].ID)
	}
	if result.Summary.Blocking < 2 {
		t.Fatalf("expected blocking summary to include synthetic unresolved issues blocker, got %+v", result.Summary)
	}
	if result.NextAction == "" {
		t.Fatal("expected next action")
	}
}
