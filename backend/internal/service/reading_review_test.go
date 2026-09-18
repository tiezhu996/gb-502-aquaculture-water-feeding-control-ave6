package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/model"
	"strings"
	"testing"
)

func TestWarningReadingKeepsSingleLevelConfirmation(t *testing.T) {
	warning := model.WaterReading{RiskLevel: constants.RiskWarning, Confirmed: false}
	if err := validateFirstVerification(warning, 1); err != nil {
		t.Fatalf("unconfirmed warning should be confirmable: %v", err)
	}
	confirmed := warning
	confirmed.Confirmed = true
	if err := validateFirstVerification(confirmed, 1); err == nil {
		t.Fatal("confirmed warning must not be confirmed again")
	}
}

func TestNormalReadingNeedsNoConfirmation(t *testing.T) {
	normal := model.WaterReading{RiskLevel: constants.RiskNormal}
	if err := validateFirstVerification(normal, 1); err == nil {
		t.Fatal("normal reading must reject confirmation")
	}
}

func TestCriticalFirstVerificationOnlyCreatesPendingReview(t *testing.T) {
	fresh := model.WaterReading{RiskLevel: constants.RiskCritical, ReviewStatus: ""}
	if err := validateFirstVerification(fresh, 1); err != nil {
		t.Fatalf("fresh critical reading should accept first verification: %v", err)
	}
	pending := model.WaterReading{RiskLevel: constants.RiskCritical, ReviewStatus: constants.ReviewPending, VerifiedByUserID: 1}
	if err := validateFirstVerification(pending, 2); err == nil {
		t.Fatal("pending critical reading must not accept another first-level verification")
	}
	approved := model.WaterReading{RiskLevel: constants.RiskCritical, ReviewStatus: constants.ReviewApproved, VerifiedByUserID: 1, ReviewedByUserID: 2}
	if err := validateFirstVerification(approved, 1); err == nil {
		t.Fatal("approved critical reading must be terminal and not re-verifiable")
	}
}

func TestCriticalSecondReviewRequiresDifferentOperator(t *testing.T) {
	pending := model.WaterReading{RiskLevel: constants.RiskCritical, ReviewStatus: constants.ReviewPending, VerifiedByUserID: 1}
	if err := validateSecondReview(pending, 1); err == nil {
		t.Fatal("same operator must not complete both levels")
	}
	if err := validateSecondReview(pending, 0); err == nil {
		t.Fatal("missing actor identity must be rejected")
	}
	if err := validateSecondReview(pending, 2); err != nil {
		t.Fatalf("a different operator should complete the second review: %v", err)
	}
}

func TestCriticalSecondReviewProducesSingleResult(t *testing.T) {
	approved := model.WaterReading{RiskLevel: constants.RiskCritical, ReviewStatus: constants.ReviewApproved, VerifiedByUserID: 1, ReviewedByUserID: 2}
	if err := validateSecondReview(approved, 3); err == nil {
		t.Fatal("duplicate/concurrent review after approval must be rejected")
	}
	rejected := model.WaterReading{RiskLevel: constants.RiskCritical, ReviewStatus: constants.ReviewRejected, VerifiedByUserID: 1, ReviewedByUserID: 2}
	if err := validateSecondReview(rejected, 3); err == nil {
		t.Fatal("duplicate/concurrent review after rejection must be rejected")
	}
}

func TestCriticalRejectionAllowsNewVerificationRound(t *testing.T) {
	rejected := model.WaterReading{RiskLevel: constants.RiskCritical, ReviewStatus: constants.ReviewRejected, VerifiedByUserID: 1, ReviewedByUserID: 2}
	if err := validateFirstVerification(rejected, 2); err != nil {
		t.Fatalf("rejected critical reading should allow a new verification round: %v", err)
	}
}

func TestCriticalReadingsBlockMessages(t *testing.T) {
	cases := map[constants.ReadingReviewStatus]string{
		constants.ReviewPending:  "等待另一名操作员复核",
		constants.ReviewRejected: "复核已被否决",
		"":                       "严重水质异常",
	}
	for status, want := range cases {
		if got := criticalReadingsBlock(status); !strings.Contains(got, want) {
			t.Fatalf("status %q message %q does not contain %q", status, got, want)
		}
	}
}
