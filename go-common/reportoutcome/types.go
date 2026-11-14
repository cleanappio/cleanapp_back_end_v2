// Package reportoutcome describes durable report review decisions separately
// from publication permissions and recorded delivery evidence.
package reportoutcome

import (
	"fmt"
	"strings"
	"time"
)

const PolicyVersion = 1

type DecisionState string

const (
	Pending          DecisionState = "pending"
	Accepted         DecisionState = "accepted"
	ClusterCandidate DecisionState = "cluster_candidate"
	NeedsInformation DecisionState = "needs_information"
	Rejected         DecisionState = "rejected"
	Held             DecisionState = "held"
)

// Handling records what a submitter authorized. It never certifies that content
// passed review or that a recipient was notified. Omission retains private
// intake semantics, including retries of older submissions.
type Handling struct {
	Publication string `json:"publication"`
	Notices     string `json:"notices"`
}

func NormalizeHandling(value *Handling) (Handling, error) {
	result := Handling{Publication: "private", Notices: "none"}
	if value == nil {
		return result, nil
	}
	if publication := strings.TrimSpace(value.Publication); publication != "" {
		result.Publication = publication
	}
	if notices := strings.TrimSpace(value.Notices); notices != "" {
		result.Notices = notices
	}
	if result.Publication != "private" && result.Publication != "automatic" {
		return Handling{}, fmt.Errorf("handling.publication must be private or automatic")
	}
	if result.Notices != "none" && result.Notices != "automatic" {
		return Handling{}, fmt.Errorf("handling.notices must be none or automatic")
	}
	return result, nil
}

type Processing struct {
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type Decision struct {
	State         DecisionState `json:"state"`
	ReasonCode    string        `json:"reason_code"`
	Message       string        `json:"message"`
	PolicyVersion int           `json:"policy_version"`
	DecidedAt     *time.Time    `json:"decided_at,omitempty"`
}

type Publication struct {
	Status        string `json:"status"`
	CanonicalPath string `json:"canonical_path,omitempty"`
	ReasonCode    string `json:"reason_code,omitempty"`
}

// Delivery exposes evidence of recorded transport outcomes, not an assertion
// that the recipient read the notice, accepted responsibility, or fixed it.
type Delivery struct {
	Channel       string     `json:"channel"`
	Status        string     `json:"status"`
	EvidenceCount int        `json:"evidence_count"`
	LastSentAt    *time.Time `json:"last_sent_at,omitempty"`
	ReasonCode    string     `json:"reason_code,omitempty"`
}

type NextAction struct {
	Type   string   `json:"type"`
	Fields []string `json:"fields,omitempty"`
}

type Candidate struct {
	Type          string `json:"type"`
	Reference     string `json:"reference"`
	CanonicalPath string `json:"canonical_path,omitempty"`
	ReasonCode    string `json:"reason_code"`
}

type Relationship struct {
	Type       string      `json:"type"`
	Candidates []Candidate `json:"candidates"`
}

type Outcome struct {
	Processing   Processing    `json:"processing"`
	Decision     *Decision     `json:"decision,omitempty"`
	Publication  Publication   `json:"publication"`
	Delivery     []Delivery    `json:"delivery"`
	NextAction   NextAction    `json:"next_action"`
	Final        bool          `json:"final"`
	Relationship *Relationship `json:"relationship,omitempty"`
}

// Analysis is a persisted English analysis plus its persisted evidence fields.
// It carries extraction results, never publication authority.
type Analysis struct {
	CompletedAt        *time.Time
	Available          bool
	Source             string
	IsValid            bool
	ReportKind         string
	EvidenceVersion    int
	EntityRelationship string
	Classification     string
	Title              string
	Description        string
	Summary            string
	BrandName          string
}

type Input struct {
	Handling           Handling
	ConsentVersion     int
	AnalysisSuppressed bool
	PolicyHoldReason   string
	PrivacyHoldReason  string
	HasOriginal        bool
	HasSourceContext   bool
	Candidates         []Candidate
}

type Record struct {
	Outcome                     Outcome
	Applicable                  bool
	PublishPending              bool
	ServerAuthorizedPublication bool
}
