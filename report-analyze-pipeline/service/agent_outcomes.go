package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"cleanapp-common/reportoutcome"
	"report-analyze-pipeline/database"
)

type agentOutcomeDatabase interface {
	AgentOutcomeInput(context.Context, int) (reportoutcome.Input, bool, error)
	CompleteAgentOutcome(context.Context, int, reportoutcome.Analysis) (reportoutcome.Record, error)
	LoadAgentReportAnalyses(context.Context, int) ([]*database.ReportAnalysis, error)
	GetAgentOutcomeReport(context.Context, int) (*database.Report, error)
	GetAgentOutcomeOriginal(context.Context, int) (*database.AgentOutcomeOriginal, error)
	AgentPublicationPending(context.Context, int) (bool, error)
	MarkAgentPublished(context.Context, int) error
	MarkAgentPublicationFailed(context.Context, int, string) error
}

type agentOutcomeRecoveryDatabase interface {
	agentOutcomeDatabase
	PendingAgentOutcomes(context.Context, int) ([]int, error)
}

var _ agentOutcomeRecoveryDatabase = (*database.Database)(nil)

type confirmedAnalysisPublisher interface {
	PublishConfirmedWithID(any, string) error
}

func (s *Service) agentOutcomeInput(seq int) (agentOutcomeDatabase, reportoutcome.Input, bool, error) {
	store, ok := s.db.(agentOutcomeDatabase)
	if !ok {
		return nil, reportoutcome.Input{}, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input, applicable, err := store.AgentOutcomeInput(ctx, seq)
	if err != nil {
		return nil, reportoutcome.Input{}, false, fmt.Errorf("load agent outcome authorization: %w", err)
	}
	return store, input, applicable, nil
}

func storedOutcomeAnalysis(analyses []*database.ReportAnalysis) reportoutcome.Analysis {
	for _, a := range analyses {
		if a != nil && a.Language == "en" {
			return reportoutcome.Analysis{Available: true, Source: a.Source,
				IsValid: a.IsValid, ReportKind: a.ReportKind, EvidenceVersion: a.EvidenceVersion,
				EntityRelationship: a.EntityRelationship, Classification: a.Classification,
				Title: a.Title, Description: a.Description, Summary: a.Summary, BrandName: a.BrandName}
		}
	}
	return reportoutcome.Analysis{}
}

// finishAgentAnalysis derives review decisions only from already persisted
// analysis and authoritative source/policy rows. It never invokes a provider.
// The caller holds the existing per-report analysis lock across publication.
func (s *Service) finishAgentAnalysis(report *database.Report, analyses []*database.ReportAnalysis, store agentOutcomeDatabase) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	record, err := store.CompleteAgentOutcome(ctx, report.Seq, storedOutcomeAnalysis(analyses))
	cancel()
	if err != nil {
		return fmt.Errorf("persist agent report outcome: %w", err)
	}
	if !record.Applicable || !record.PublishPending {
		return nil
	}
	if !record.ServerAuthorizedPublication || record.Outcome.Publication.Status != "public" {
		return fmt.Errorf("agent publication pending without server authorization")
	}
	if !storedOutcomeAnalysis(analyses).Available {
		return fmt.Errorf("agent publication pending without stored English analysis")
	}
	// Recheck live visibility at the last possible point. A public event can
	// feed renderers; each transport/reward consumer must still apply its own
	// policy, including the permanent suppress_rewards flag for agent intake.
	visibility, err := s.db.ReportVisibility(report.Seq)
	if err != nil || visibility != "public" {
		return fmt.Errorf("agent public visibility is not confirmed")
	}
	grantCtx, grantCancel := context.WithTimeout(context.Background(), 10*time.Second)
	pending, grantErr := store.AgentPublicationPending(grantCtx, report.Seq)
	grantCancel()
	if grantErr != nil {
		return fmt.Errorf("recheck agent publication authorization: %w", grantErr)
	}
	if !pending {
		return fmt.Errorf("agent publication authorization changed before broker publication")
	}
	if err := s.publishAnalyzedReportWithAuthorization(report, analyses, true); err != nil {
		markCtx, markCancel := context.WithTimeout(context.Background(), 10*time.Second)
		markErr := store.MarkAgentPublicationFailed(markCtx, report.Seq, "public_event_retry_pending")
		markCancel()
		if markErr != nil {
			return fmt.Errorf("record agent publication retry: %w", markErr)
		}
		return fmt.Errorf("publish agent analysis event: %w", err)
	}
	markCtx, markCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer markCancel()
	if err := store.MarkAgentPublished(markCtx, report.Seq); err != nil {
		// Do not acknowledge the triggering delivery after an unrecorded
		// completion. A replay reuses these analyses and the same event ID.
		return fmt.Errorf("persist confirmed agent publication: %w", err)
	}
	return nil
}

const agentOutcomeRecoveryLimit = 5

// Only the machine worker enables this metadata-only recovery loop by default.
// Every batch is bounded and a busy broker cannot trigger another provider pass.
func (s *Service) processPendingAgentOutcomes() {
	defer s.agentOutcomeWorkers.Done()
	for {
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-s.stopChan:
			timer.Stop()
			return
		case <-timer.C:
		}
		ctx, cancel := context.WithCancel(context.Background())
		watcherDone := make(chan struct{})
		go func() {
			defer close(watcherDone)
			select {
			case <-s.stopChan:
				cancel()
			case <-ctx.Done():
			}
		}()
		if err := s.processAgentOutcomeBatch(ctx); err != nil && ctx.Err() == nil {
			log.Printf("Agent outcome recovery remains pending: %v", err)
		}
		cancel()
		<-watcherDone
	}
}

func (s *Service) processAgentOutcomeBatch(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store, ok := s.db.(agentOutcomeRecoveryDatabase)
	if !ok {
		return nil
	}
	scanCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	sequences, err := store.PendingAgentOutcomes(scanCtx, agentOutcomeRecoveryLimit)
	cancel()
	if err != nil {
		return fmt.Errorf("select bounded agent outcomes: %w", err)
	}
	if len(sequences) > agentOutcomeRecoveryLimit {
		return fmt.Errorf("agent outcome recovery exceeded its batch bound")
	}
	for _, seq := range sequences {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.recoverAgentOutcome(ctx, store, seq); err != nil {
			// Continue this finite batch so a broken record cannot starve the
			// others. The shared store durably paces their next attempts.
			log.Printf("Agent outcome %d remains pending: %v", seq, err)
		}
	}
	return nil
}

func (s *Service) recoverAgentOutcome(ctx context.Context, store agentOutcomeDatabase, seq int) error {
	lockCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	lock, _, err := s.db.LockReportAnalysis(lockCtx, seq)
	cancel()
	if err != nil {
		return err
	}
	defer lock.Release()
	readCtx, readCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readCancel()
	input, applicable, err := store.AgentOutcomeInput(readCtx, seq)
	if err != nil || !applicable {
		return err
	}
	report, err := store.GetAgentOutcomeReport(readCtx, seq)
	if err != nil {
		return err
	}
	analyses, err := store.LoadAgentReportAnalyses(readCtx, seq)
	if err != nil {
		return err
	}
	if !storedOutcomeAnalysis(analyses).Available && !input.AnalysisSuppressed &&
		!(input.ConsentVersion > 0 && input.PrivacyHoldReason != "" &&
			(input.Handling.Publication == "automatic" || input.Handling.Notices == "automatic")) {
		// Model work still belongs to the original bounded broker consumer.
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.finishAgentAnalysis(report, analyses, store)
}
