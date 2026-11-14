package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cleanapp-common/effectpolicy"
	"cleanapp-common/evidence"
	"cleanapp-common/reportoutcome"
	"github.com/DATA-DOG/go-sqlmock"
	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/services"
)

type agentOutcomeStoreFixture struct {
	*analysisStore
	input              reportoutcome.Input
	record             reportoutcome.Record
	stored             []*database.ReportAnalysis
	inputErr           error
	loadErr            error
	completeErr        error
	markErr            error
	markFailErr        error
	completeInputs     []reportoutcome.Analysis
	marked             int
	failed             int
	alreadyPublished   bool
	commitDespiteError bool
	visibility         string
	pending            []int
	pendingErr         error
	scanContext        context.Context
	scanLimit          int
	grantErr           error
	grantRevoked       bool
	grantChecks        int
	lateHold           bool
	original           database.AgentOutcomeOriginal
	originalErr        error
	originalCalls      int
}

func (d *agentOutcomeStoreFixture) AgentOutcomeInput(context.Context, int) (reportoutcome.Input, bool, error) {
	return d.input, true, d.inputErr
}
func (d *agentOutcomeStoreFixture) LockReportAnalysis(context.Context, int) (*database.ReportAnalysisLock, bool, error) {
	return &database.ReportAnalysisLock{}, d.alreadyPublished, nil
}
func (d *agentOutcomeStoreFixture) GetAgentOutcomeReport(context.Context, int) (*database.Report, error) {
	return d.report, nil
}
func (d *agentOutcomeStoreFixture) GetAgentOutcomeOriginal(context.Context, int) (*database.AgentOutcomeOriginal, error) {
	d.originalCalls++
	copy := d.original
	return &copy, d.originalErr
}
func (d *agentOutcomeStoreFixture) LoadAgentReportAnalyses(context.Context, int) ([]*database.ReportAnalysis, error) {
	if d.loadErr != nil {
		return nil, d.loadErr
	}
	if d.stored != nil {
		return d.stored, nil
	}
	var result []*database.ReportAnalysis
	for _, a := range d.analyses {
		copy := *a
		copy.EvidenceVersion = d.evidenceVersion
		if copy.EvidenceVersion == 0 {
			copy.Fields = evidence.Fields{ReportKind: "unknown", EntityRelationship: "unknown"}
		}
		result = append(result, &copy)
	}
	return result, nil
}
func (d *agentOutcomeStoreFixture) CompleteAgentOutcome(_ context.Context, _ int, analysis reportoutcome.Analysis) (reportoutcome.Record, error) {
	d.completeInputs = append(d.completeInputs, analysis)
	if d.lateHold {
		d.grantRevoked = true
	}
	return d.record, d.completeErr
}
func (d *agentOutcomeStoreFixture) ReportVisibility(int) (string, error) { return d.visibility, nil }
func (d *agentOutcomeStoreFixture) AgentPublicationPending(context.Context, int) (bool, error) {
	d.grantChecks++
	return d.record.PublishPending && !d.grantRevoked, d.grantErr
}
func (d *agentOutcomeStoreFixture) PendingAgentOutcomes(ctx context.Context, limit int) ([]int, error) {
	d.scanContext, d.scanLimit = ctx, limit
	return d.pending, d.pendingErr
}
func (d *agentOutcomeStoreFixture) MarkAgentPublished(context.Context, int) error {
	if d.markErr == nil || d.commitDespiteError {
		d.record.PublishPending = false
		d.alreadyPublished = true
	}
	if d.markErr != nil {
		return d.markErr
	}
	d.marked++
	return nil
}
func (d *agentOutcomeStoreFixture) MarkAgentPublicationFailed(context.Context, int, string) error {
	d.failed++
	return d.markFailErr
}

type agentAnalysisClientFixture struct {
	*analysisClient
	imageCalls       int
	translationCalls int
	input            string
}

func (c *agentAnalysisClientFixture) AnalyzeImage(image []byte, input string) (string, error) {
	c.imageCalls++
	c.input = input
	return c.analysisClient.AnalyzeImage(image, input)
}
func (c *agentAnalysisClientFixture) TranslateAnalysis(value, language string) (string, error) {
	c.translationCalls++
	return c.analysisClient.TranslateAnalysis(value, language)
}

type confirmedAgentPublisherFixture struct {
	*recordingPublisher
	err error
	ids []string
}

func (p *confirmedAgentPublisherFixture) PublishConfirmedWithID(value any, id string) error {
	p.ids = append(p.ids, id)
	if p.err != nil {
		return p.err
	}
	return p.recordingPublisher.Publish(value)
}

func newAgentOutcomeFixture() *agentOutcomeStoreFixture {
	return &agentOutcomeStoreFixture{
		analysisStore: &analysisStore{report: &database.Report{Seq: 84, Description: "canonical retained report"}},
		input:         reportoutcome.Input{Handling: reportoutcome.Handling{Publication: "private", Notices: "none"}},
		record: reportoutcome.Record{Applicable: true, Outcome: reportoutcome.Outcome{
			Decision: &reportoutcome.Decision{State: reportoutcome.Accepted}, Publication: reportoutcome.Publication{Status: "private"}, Final: true}},
		visibility: "shadow",
		original: database.AgentOutcomeOriginal{Original: evidence.Original{Text: "I hit an error on submit.", SourceType: "machine_submission"},
			Context: []byte(`{"description":"I hit an error on submit.","digital_context":{"product_or_site":"Fixture"}}`), Channel: "wire"},
	}
}

func storedAgentAnalysisFixture() []*database.ReportAnalysis {
	return []*database.ReportAnalysis{{Seq: 84, Source: "stored-provider", Language: "en", IsValid: true,
		Fields: evidence.Fields{ReportKind: "firsthand", EntityRelationship: "affected"}, EvidenceVersion: 1,
		Classification: "digital", Title: "Retained checkout failure", Description: "The button failed."}}
}

func authorizeAgentPublicationFixture(store *agentOutcomeStoreFixture) {
	store.stored = storedAgentAnalysisFixture()
	store.visibility = "public"
	store.input.Handling = reportoutcome.Handling{Publication: "automatic", Notices: "none"}
	store.input.ConsentVersion = 1
	store.record.PublishPending = true
	store.record.ServerAuthorizedPublication = true
	store.record.Outcome.Publication.Status = "public"
	// The precise server-authorized event exemption must not turn rewards on.
	store.policy = map[effectpolicy.Effect]bool{effectpolicy.Rewards: true, effectpolicy.Routing: true, effectpolicy.Notifications: true}
}

func TestPrivateAgentBrokerReplayReusesStoredAnalysisWithoutAnotherProviderPass(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := newAgentOutcomeFixture()
	store.sqlDB = db
	client := &agentAnalysisClientFixture{analysisClient: &analysisClient{}}
	publisher := &confirmedAgentPublisherFixture{recordingPublisher: &recordingPublisher{}}
	svc := &Service{config: &config.Config{}, db: store, llmClient: client,
		brandService: services.NewBrandService(), publisher: publisher}
	if err := svc.AnalyzeReport(&database.Report{Seq: 84, Description: "untrusted queued projection"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AnalyzeReport(&database.Report{Seq: 84}); err != nil {
		t.Fatal(err)
	}
	if client.imageCalls != 1 || client.calls != 1 || client.translationCalls != 0 || len(store.analyses) != 1 || len(store.evidenceWrites) != 1 {
		t.Fatal("a private replay repeated provider work or rewrote stored analysis")
	}
	if store.originalCalls != 1 {
		t.Fatal("a stored English replay re-read or replaced the retained original")
	}
	if len(store.completeInputs) != 2 || !store.completeInputs[1].Available || store.completeInputs[1].EvidenceVersion != 1 ||
		len(publisher.events) != 0 || store.marked != 0 {
		t.Fatal("private outcome was not retained independently of publication")
	}
	if !strings.Contains(client.input, "I hit an error on submit.") || strings.Contains(client.input, "untrusted queued projection") {
		t.Fatal("model input used the queue projection instead of the retained original")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRetryAfterSavedProviderWorkDoesNotRepeatModels(t *testing.T) {
	for _, failure := range []string{"evidence storage", "outcome storage"} {
		t.Run(failure, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			store := newAgentOutcomeFixture()
			store.sqlDB = db
			if failure == "evidence storage" {
				store.evidenceSaveErr = errors.New("evidence save unavailable")
				store.record.Outcome.Decision.State = reportoutcome.Held
			} else {
				store.completeErr = errors.New("outcome save unavailable")
			}
			client := &agentAnalysisClientFixture{analysisClient: &analysisClient{}}
			svc := &Service{config: &config.Config{}, db: store, llmClient: client, brandService: services.NewBrandService()}
			if err := svc.AnalyzeReport(store.report); err == nil {
				t.Fatal("failed persistence was silently acknowledged")
			}
			store.evidenceSaveErr, store.completeErr = nil, nil
			if err := svc.AnalyzeReport(store.report); err != nil {
				t.Fatal(err)
			}
			if client.imageCalls != 1 || client.calls != 1 || len(store.analyses) != 1 || len(store.evidenceWrites) != 1 {
				t.Fatal("retry repeated paid provider work after its English result was saved")
			}
			completed := store.completeInputs[len(store.completeInputs)-1]
			if !completed.Available || (failure == "evidence storage" && completed.EvidenceVersion != 0) {
				t.Fatal("recovery guessed missing evidence instead of preserving uncertainty")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAgentOutcomeFailureAndPublicationRetryNeverCallProviders(t *testing.T) {
	store := newAgentOutcomeFixture()
	authorizeAgentPublicationFixture(store)
	publisher := &confirmedAgentPublisherFixture{recordingPublisher: &recordingPublisher{}, err: errors.New("broker unavailable")}
	// A nil LLM is deliberate: every recovery path must use persisted work.
	svc := &Service{db: store, publisher: publisher}
	store.completeErr = errors.New("outcome database unavailable")
	if err := svc.AnalyzeReport(store.report); err == nil || len(publisher.ids) != 0 {
		t.Fatal("outcome persistence failure was acknowledged or published")
	}
	store.completeErr = nil
	if err := svc.AnalyzeReport(store.report); err == nil || store.failed != 1 || store.marked != 0 {
		t.Fatal("broker failure was silently acknowledged or marked complete")
	}
	publisher.err = nil
	if err := svc.AnalyzeReport(store.report); err != nil {
		t.Fatal(err)
	}
	if err := svc.AnalyzeReport(store.report); err != nil {
		t.Fatal(err)
	}
	if len(publisher.events) != 1 || store.marked != 1 || len(publisher.ids) != 2 || publisher.ids[0] != publisher.ids[1] {
		t.Fatal("recovery lost event identity or repeated completed publication")
	}
	if len(store.analyses) != 0 || len(store.evidenceWrites) != 0 || !store.policy[effectpolicy.Rewards] {
		t.Fatal("outcome recovery repeated analysis or enabled rewards")
	}
	if publisher.events[0].Report.Description != store.report.Description {
		t.Fatal("recovery event did not use the canonical report")
	}
}

func TestAgentPublicationMarkerFailureIsRetryableAndCommittedReplayDoesNotRepublish(t *testing.T) {
	store := newAgentOutcomeFixture()
	authorizeAgentPublicationFixture(store)
	store.markErr = errors.New("commit acknowledgement lost")
	store.commitDespiteError = true
	publisher := &confirmedAgentPublisherFixture{recordingPublisher: &recordingPublisher{}}
	svc := &Service{db: store, publisher: publisher}
	if err := svc.AnalyzeReport(store.report); err == nil {
		t.Fatal("unconfirmed durable marker was silently acknowledged")
	}
	store.markErr = nil
	if err := svc.AnalyzeReport(store.report); err != nil {
		t.Fatal(err)
	}
	if len(publisher.events) != 1 || len(publisher.ids) != 1 {
		t.Fatal("a committed but previously unconfirmed marker caused another publication")
	}
}

func TestAgentOutcomeAuthorizationAndLookupErrorsFailClosedBeforeProviderOrPublication(t *testing.T) {
	for _, mode := range []string{"input failure", "stored lookup failure", "retained original failure", "private visibility", "missing authority", "late policy hold", "late authorization lookup failure", "privacy hold", "analysis hold"} {
		t.Run(mode, func(t *testing.T) {
			store := newAgentOutcomeFixture()
			authorizeAgentPublicationFixture(store)
			switch mode {
			case "input failure":
				store.inputErr = errors.New("authorization database unavailable")
			case "stored lookup failure":
				store.loadErr = errors.New("analysis lookup unavailable")
			case "retained original failure":
				store.stored = nil
				store.originalErr = errors.New("bounded retained original unavailable")
			case "private visibility":
				store.visibility = "shadow"
			case "missing authority":
				store.record.ServerAuthorizedPublication = false
			case "late policy hold":
				store.lateHold = true
			case "late authorization lookup failure":
				store.grantErr = errors.New("fresh policy lookup unavailable")
			case "privacy hold", "analysis hold":
				store.stored = nil
				store.record.PublishPending = false
				store.record.ServerAuthorizedPublication = false
				store.record.Outcome.Publication.Status = "private"
				store.record.Outcome.Decision.State = reportoutcome.Held
				if mode == "privacy hold" {
					store.input.PrivacyHoldReason = "redaction_required"
				} else {
					store.policy[effectpolicy.Analysis] = true
				}
			}
			publisher := &confirmedAgentPublisherFixture{recordingPublisher: &recordingPublisher{}}
			err := (&Service{db: store, publisher: publisher}).AnalyzeReport(store.report)
			wantError := mode != "privacy hold" && mode != "analysis hold"
			if (err != nil) != wantError || len(publisher.events) != 0 || store.marked != 0 || len(store.analyses) != 0 {
				t.Fatalf("agent guard %s failed closed: err=%v", mode, err)
			}
			if !wantError && (len(store.completeInputs) != 1 || store.completeInputs[0].Available) {
				t.Fatal("known hold did not persist a non-model outcome")
			}
			if (mode == "late policy hold" || mode == "late authorization lookup failure") &&
				(len(store.completeInputs) != 1 || store.grantChecks != 1) {
				t.Fatal("late policy change was not rechecked after completing the decision")
			}
		})
	}
}

func TestBoundedOutcomeRecoveryUsesOnlyStoredAnalysisAndClearsCompletedPublication(t *testing.T) {
	store := newAgentOutcomeFixture()
	authorizeAgentPublicationFixture(store)
	store.pending = []int{84}
	publisher := &confirmedAgentPublisherFixture{recordingPublisher: &recordingPublisher{}}
	svc := &Service{db: store, publisher: publisher}
	started := time.Now()
	if err := svc.processAgentOutcomeBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.processAgentOutcomeBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline, bounded := store.scanContext.Deadline()
	if !bounded || deadline.Sub(started) > 8*time.Second+100*time.Millisecond || store.scanContext.Err() != context.Canceled ||
		store.scanLimit != agentOutcomeRecoveryLimit {
		t.Fatal("recovery scan lost its time/result bound or retained its query context")
	}
	if len(publisher.events) != 1 || store.marked != 1 || len(store.analyses) != 0 || len(store.evidenceWrites) != 0 {
		t.Fatal("metadata recovery repeated completed publication or provider work")
	}
}

func TestOutcomeRecoveryNeverStartsMissingModelWorkOrUnboundedBatches(t *testing.T) {
	for _, mode := range []string{"analysis not yet saved", "scan failure", "too many candidates", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			store := newAgentOutcomeFixture()
			store.pending = []int{84}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "scan failure":
				store.pendingErr = errors.New("fixture query failed")
			case "too many candidates":
				store.pending = []int{1, 2, 3, 4, 5, 6}
			case "cancelled":
				cancel()
			}
			publisher := &confirmedAgentPublisherFixture{recordingPublisher: &recordingPublisher{}}
			err := (&Service{db: store, publisher: publisher}).processAgentOutcomeBatch(ctx)
			if (err != nil) != (mode != "analysis not yet saved") {
				t.Fatalf("recovery %s returned %v", mode, err)
			}
			if len(store.completeInputs) != 0 || len(publisher.ids) != 0 || len(store.analyses) != 0 || len(store.evidenceWrites) != 0 {
				t.Fatal("recovery invented work or published after a bounded-query failure")
			}
		})
	}
}

func TestAgentOutcomeRecoveryStopsWithoutWaitingForItsNextPoll(t *testing.T) {
	store := newAgentOutcomeFixture()
	svc := &Service{config: &config.Config{AgentOutcomeWorkerEnabled: true}, db: store,
		stopChan: make(chan bool), publisher: &confirmedAgentPublisherFixture{recordingPublisher: &recordingPublisher{}}}
	svc.Start()
	done := make(chan struct{})
	go func() { svc.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for the metadata recovery poll interval")
	}
	if len(store.completeInputs) != 0 || len(store.analyses) != 0 {
		t.Fatal("stopped recovery worker started analysis or outcome work")
	}
}
