// replay-privacy-holds is a finite operator maintenance command. It never
// changes reports, permissions or outcomes and never constructs a model client.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cleanapp-common/reportoutcome"
	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/rabbitmq"
)

const reason = "payment_information_detected"
const manifestKind = "cleanapp-payment-hold-replay.v1"

type entry struct {
	Seq        int    `json:"seq"`
	ReceiptID  string `json:"receipt_id"`
	SourceHash string `json:"source_sha256"`
	StateHash  string `json:"state_sha256"`
}
type manifest struct {
	Kind   string  `json:"kind"`
	Reason string  `json:"expected_reason"`
	Items  []entry `json:"items"`
}
type snapshot struct {
	Entry                                                      entry
	ReceiptOK, Private, Unpublished, NoAnalysis, NoImage, Held bool
	Input                                                      reportoutcome.Input
	Applicable                                                 bool
}
type repository interface {
	Load(context.Context, int) (snapshot, error)
	Lock(context.Context, int) (func() error, bool, error)
}
type realRepository struct{ db *database.Database }

func (r realRepository) Lock(ctx context.Context, seq int) (func() error, bool, error) {
	lock, published, err := r.db.LockReportAnalysis(ctx, seq)
	if err != nil {
		return nil, false, err
	}
	return lock.Release, published, nil
}
func (r realRepository) Load(ctx context.Context, seq int) (snapshot, error) {
	s := snapshot{Entry: entry{Seq: seq}}
	// Point lookups only. Hash retained content in SQL; never return it to the
	// command. Explicit UTF8MB4 makes operator manifest comparisons portable.
	err := r.db.GetDB().QueryRowContext(ctx, `SELECT w.receipt_id,
	 SHA2(CAST(JSON_OBJECT('submission',w.submission_id,'source',w.source_id,'fetcher',w.fetcher_id,
	 'report',w.report_json,'agent',w.agent_json,'provenance',w.provenance_json,'delivery',w.delivery_json,
	 'retained_description',SHA2(COALESCE(r.description,''),256),'report_id',r.id,'reported_at',r.ts,
	 'latitude',r.latitude,'longitude',r.longitude)
	 AS CHAR CHARACTER SET utf8mb4),256),
	 SHA2(CAST(JSON_OBJECT('receipt',rc.receipt_id,'receipt_status',rc.status,'lane',rc.lane,
	 'outcome',o.outcome_json,'consent',o.consent_version,'publication',o.publication_mode,'notices',o.notices_mode,
	 'authorized',o.publication_authorized,'event',o.public_event_status,'visibility',rr.visibility,
	 'trust',rr.trust_level,'published',rr.analysed_published_at,'promoted',rr.promoted_to_public_at,
	 'policy',(SELECT JSON_OBJECT('namespace',p.source_namespace,'reason',p.reason,'version',p.policy_version,
	 'analysis',p.suppress_analysis,'email',p.suppress_email,'notifications',p.suppress_notifications,
	 'routing',p.suppress_routing,'rewards',p.suppress_rewards)
	 FROM report_side_effect_policy p WHERE p.report_seq=rr.report_seq)) AS CHAR CHARACTER SET utf8mb4),256),
	 w.actor_kind='machine' AND w.channel IN ('wire','public_agent') AND rc.status IN ('quarantined','accepted'),
	 rr.visibility IN ('shadow','private','quarantine','quarantined'),
	 rr.analysed_published_at IS NULL AND rr.promoted_to_public_at IS NULL,
	 NOT EXISTS(SELECT 1 FROM report_analysis a WHERE a.seq=r.seq),COALESCE(OCTET_LENGTH(r.image),0)=0,
	 o.processing_status='not_required' AND o.decision_state='held' AND o.reason_code=?
	 AND o.consent_version=1 AND o.publication_mode='automatic' AND o.notices_mode='automatic'
	 AND o.publication_authorized=FALSE AND o.public_event_status='none'
	 AND o.publication_status IN ('private','held')
	 FROM report_raw rr JOIN reports r ON r.seq=rr.report_seq
	 JOIN wire_submissions_raw w ON w.report_seq=rr.report_seq AND w.fetcher_id=rr.fetcher_id
	 JOIN wire_submission_receipts rc ON rc.receipt_id=w.receipt_id AND rc.submission_id=w.submission_id
	 AND rc.fetcher_id=w.fetcher_id AND rc.report_seq=w.report_seq
	 JOIN report_outcomes o ON o.report_seq=rr.report_seq
	 WHERE rr.report_seq=? ORDER BY w.submitted_at DESC,w.submission_id DESC LIMIT 1`, reason, seq).
		Scan(&s.Entry.ReceiptID, &s.Entry.SourceHash, &s.Entry.StateHash, &s.ReceiptOK, &s.Private, &s.Unpublished, &s.NoAnalysis, &s.NoImage, &s.Held)
	if err != nil {
		return s, err
	}
	s.Input, s.Applicable, err = r.db.AgentOutcomeInput(ctx, seq)
	return s, err
}

func validate(s snapshot) error {
	if !s.ReceiptOK || !s.Private || !s.Unpublished || !s.NoAnalysis || !s.NoImage || !s.Held {
		return errors.New("expected_unanalyzed_private_payment_hold_missing")
	}
	i := s.Input
	if !s.Applicable || i.ConsentVersion != 1 || i.Handling.Publication != "automatic" || i.Handling.Notices != "automatic" || i.AnalysisSuppressed || i.PolicyHoldReason != "" || i.PrivacyHoldReason != "" || !i.HasOriginal || !i.HasSourceContext {
		return errors.New("current_review_does_not_permit_analysis")
	}
	if s.Entry.Seq <= 0 || !strings.HasPrefix(s.Entry.ReceiptID, "rcpt_") || !hashString(s.Entry.SourceHash) || !hashString(s.Entry.StateHash) {
		return errors.New("snapshot_identity_invalid")
	}
	return nil
}
func hashString(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == 32 && len(s) == 64
}
func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func parseSeqs(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	if len(parts) < 1 || len(parts) > 10 {
		return nil, errors.New("provide_one_to_ten_explicit_sequences")
	}
	seen := map[int]bool{}
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n <= 0 || n > 2147483647 || seen[n] {
			return nil, errors.New("sequences_must_be_unique_positive_integers")
		}
		seen[n] = true
		result = append(result, n)
	}
	sort.Ints(result)
	return result, nil
}
func review(ctx context.Context, r repository, seqs []int) (manifest, error) {
	m := manifest{Kind: manifestKind, Reason: reason}
	for _, seq := range seqs {
		s, err := r.Load(ctx, seq)
		if err != nil {
			return m, errors.New("snapshot_read_failed")
		}
		if err = validate(s); err != nil {
			return m, err
		}
		m.Items = append(m.Items, s.Entry)
	}
	return m, nil
}
func validateManifest(m manifest, seqs []int) error {
	if m.Kind != manifestKind || m.Reason != reason || len(m.Items) != len(seqs) {
		return errors.New("reviewed_manifest_mismatch")
	}
	for i, e := range m.Items {
		if e.Seq != seqs[i] || !strings.HasPrefix(e.ReceiptID, "rcpt_") || !hashString(e.SourceHash) || !hashString(e.StateHash) {
			return errors.New("reviewed_manifest_mismatch")
		}
	}
	return nil
}

type journal struct {
	ManifestHash string         `json:"manifest_sha256"`
	Items        map[int]string `json:"items"`
}
type summary struct {
	Confirmed        []int  `json:"confirmed"`
	AlreadyConfirmed []int  `json:"already_confirmed"`
	Error            string `json:"error,omitempty"`
	ProviderCalls    int    `json:"provider_calls"`
}

func replay(ctx context.Context, r repository, m manifest, j *journal, persist func() error, publish func(int, string) error) (result summary) {
	for _, e := range m.Items {
		if j.Items[e.Seq] == "confirmed" {
			result.AlreadyConfirmed = append(result.AlreadyConfirmed, e.Seq)
			continue
		}
		if j.Items[e.Seq] != "" {
			result.Error = "prior_attempt_requires_operator_review"
			return
		}
		code := func() (code string) {
			unlock, published, err := r.Lock(ctx, e.Seq)
			if err != nil {
				return "analysis_lock_failed"
			}
			defer func() {
				if unlock() != nil && code == "" {
					code = "analysis_lock_release_failed"
				}
			}()
			if published {
				return "report_already_published"
			}
			s, err := r.Load(ctx, e.Seq)
			if err != nil {
				return "locked_snapshot_read_failed"
			}
			if err = validate(s); err != nil {
				return err.Error()
			}
			if s.Entry != e {
				return "reviewed_source_or_state_changed"
			}
			j.Items[e.Seq] = "attempted"
			if persist() != nil {
				return "journal_write_failed"
			}
			if publish(e.Seq, "privacy-hold-replay:v1:"+strconv.Itoa(e.Seq)+":"+e.SourceHash) != nil {
				return "broker_outcome_uncertain"
			}
			result.Confirmed = append(result.Confirmed, e.Seq)
			j.Items[e.Seq] = "confirmed"
			if persist() != nil {
				return "confirmed_journal_write_failed"
			}
			return ""
		}()
		if code != "" {
			result.Error = code
			return
		}
	}
	return
}

func readPrivateJSON(path string, dst any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 32768 {
		return errors.New("private_bounded_file_required")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 32769))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(dst); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("single_json_document_required")
	}
	return nil
}
func saveJournal(path string, j journal) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".privacy-replay-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func run() int {
	seqFlag := flag.String("seqs", "", "Required explicit comma-separated sequences (max10)")
	apply := flag.Bool("apply", false, "Enqueue only the reviewed existing reports after deployment")
	manifestPath := flag.String("manifest", "", "Reviewed private dry-run JSON file (required for apply)")
	journalPath := flag.String("journal", "", "Private absolute journal path (required for apply)")
	flag.Parse()
	log.SetOutput(io.Discard)
	fail := func(code string) int { _ = json.NewEncoder(os.Stdout).Encode(summary{Error: code}); return 1 }
	seqs, err := parseSeqs(*seqFlag)
	if err != nil || flag.NArg() != 0 {
		return fail("explicit_sequences_required")
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("configuration_unavailable")
	}
	if cfg.RabbitMQ.Exchange != "cleanapp-exchange" || cfg.RabbitMQ.RawReportRoutingKey != "report.raw" || cfg.RabbitMQ.Queue != "report-analysis-queue" {
		return fail("canonical_machine_queue_configuration_required")
	}
	db, err := database.NewDatabase(cfg)
	if err != nil {
		return fail("database_unavailable")
	}
	defer db.Close()
	repo := realRepository{db}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if !*apply {
		m, err := review(ctx, repo, seqs)
		if err != nil {
			return fail(err.Error())
		}
		_ = json.NewEncoder(os.Stdout).Encode(m)
		return 0
	}
	var m manifest
	if readPrivateJSON(*manifestPath, &m) != nil || validateManifest(m, seqs) != nil {
		return fail("reviewed_private_manifest_required")
	}
	if !filepath.IsAbs(*journalPath) {
		return fail("absolute_journal_required")
	}
	parent, err := os.Lstat(filepath.Dir(*journalPath))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0077 != 0 {
		return fail("private_journal_directory_required")
	}
	lock, err := os.OpenFile(*journalPath+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fail("journal_lock_unavailable")
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return fail("journal_busy")
	}
	j := journal{ManifestHash: digest(m), Items: map[int]string{}}
	if _, err = os.Lstat(*journalPath); err == nil {
		if readPrivateJSON(*journalPath, &j) != nil {
			return fail("private_journal_invalid")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail("private_journal_invalid")
	}
	if j.ManifestHash != digest(m) || j.Items == nil {
		return fail("journal_manifest_mismatch")
	}
	for seq, phase := range j.Items {
		found := false
		for _, e := range m.Items {
			found = found || e.Seq == seq
		}
		if !found || (phase != "attempted" && phase != "confirmed") {
			return fail("private_journal_invalid")
		}
	}
	var publisher *rabbitmq.Publisher
	defer func() {
		if publisher != nil {
			_ = publisher.Close()
		}
	}()
	result := replay(ctx, repo, m, &j, func() error { return saveJournal(*journalPath, j) }, func(seq int, id string) error {
		if publisher == nil {
			var err error
			publisher, err = rabbitmq.NewPublisher(cfg.RabbitMQ.GetAMQPURL(), cfg.RabbitMQ.Exchange, cfg.RabbitMQ.RawReportRoutingKey)
			if err != nil {
				return err
			}
		}
		return publisher.PublishConfirmedWithID(struct {
			Seq int `json:"seq"`
		}{seq}, id)
	})
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if result.Error != "" {
		return 1
	}
	return 0
}
func main() { os.Exit(run()) }
