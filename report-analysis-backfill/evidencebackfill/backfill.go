// Package evidencebackfill reuses the production original resolver/classifier.
// Dry runs never call Save, never publish routing events, and never modify raw data.
package evidencebackfill

import (
	"cleanapp-common/evidence"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type Options struct {
	Brands     []string
	Limit      int
	AfterSeq   int
	ThroughSeq int
	ShardCount int
	ShardIndex int
	Apply      bool
	Force      bool
	Interval   time.Duration
}

func (o Options) Validate() error {
	if len(o.Brands) == 0 {
		return fmt.Errorf("at least one brand is required")
	}
	if o.Limit < 1 || o.Limit > 10000 {
		return fmt.Errorf("limit must be between 1 and 10000")
	}
	if o.AfterSeq < 0 {
		return fmt.Errorf("after-seq must be nonnegative")
	}
	if o.ThroughSeq < 0 || (o.ThroughSeq > 0 && o.ThroughSeq < o.AfterSeq) {
		return fmt.Errorf("through-seq must be nonnegative and at least after-seq")
	}
	shards := o.ShardCount
	if shards == 0 {
		shards = 1
	}
	if shards < 1 || shards > 8 || o.ShardIndex < 0 || o.ShardIndex >= shards {
		return fmt.Errorf("shard-count must be between 1 and 8 and shard-index must be within the shard count")
	}
	if o.Interval < 0 {
		return fmt.Errorf("interval must be nonnegative")
	}
	return nil
}

type Candidate struct {
	Seq   int    `json:"seq"`
	Brand string `json:"brand"`
	Title string `json:"title"`
}
type Store interface {
	Candidates(context.Context, Options) ([]Candidate, error)
	Original(context.Context, int) (evidence.Original, error)
	Save(context.Context, int, evidence.Fields) error
}
type Result struct {
	DryRun      bool           `json:"dry_run"`
	Examined    int            `json:"examined"`
	Written     int            `json:"written"`
	Failed      int            `json:"failed"`
	LabelCounts map[string]int `json:"label_counts"`
	LastSeq     int            `json:"last_seq"`
}

func Run(ctx context.Context, opts Options, store Store, classifier evidence.Classifier, out io.Writer) (Result, error) {
	result := Result{DryRun: !opts.Apply, LabelCounts: map[string]int{"firsthand": 0, "coverage": 0, "unknown": 0}}
	if err := opts.Validate(); err != nil {
		return result, err
	}
	rows, err := store.Candidates(ctx, opts)
	if err != nil {
		return result, err
	}
	encoder := json.NewEncoder(out)
	for i, row := range rows {
		if i > 0 && opts.Interval > 0 {
			timer := time.NewTimer(opts.Interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, ctx.Err()
			case <-timer.C:
			}
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		original, err := store.Original(ctx, row.Seq)
		originalResolved := err == nil
		fields := evidence.Unknown("Stored original could not be resolved.")
		if err == nil {
			fields, err = evidence.Classify(classifier, original, row.Brand)
		}
		result.Examined++
		result.LastSeq = row.Seq
		result.LabelCounts[fields.ReportKind]++
		item := struct {
			Candidate
			SourceType             string          `json:"source_type"`
			OriginalTextAvailable  bool            `json:"original_text_available"`
			OriginalMediaAvailable bool            `json:"original_media_available"`
			Evidence               evidence.Fields `json:"evidence"`
			Error                  string          `json:"error,omitempty"`
		}{Candidate: row, SourceType: reviewSourceType(original.SourceType), OriginalTextAvailable: originalResolved && strings.TrimSpace(original.Text) != "", OriginalMediaAvailable: originalResolved && evidence.ValidOriginalImage(original.Image), Evidence: fields}
		if !originalResolved {
			item.SourceType = "unknown"
		}
		if original.RequiresRedaction {
			// This is a review output copy: persisted extraction is unchanged, and the
			// public API independently applies its own privacy guards.
			item.Title = "[Title withheld: source requires redaction]"
			item.Evidence.EvidenceExcerpt = ""
			item.Evidence.ReportedImpact = ""
			item.Evidence.ClassificationReason = "Original evidence requires redaction; extracted text omitted from review output."
		}
		if err != nil {
			result.Failed++
			item.Error = "Original resolution or evidence classification failed; row was not written."
		} else if opts.Apply {
			if err = store.Save(ctx, row.Seq, fields); err != nil {
				return result, fmt.Errorf("save evidence for seq %d: %w", row.Seq, err)
			}
			result.Written++
		}
		if err := encoder.Encode(item); err != nil {
			return result, err
		}
	}
	if err := encoder.Encode(result); err != nil {
		return result, err
	}
	return result, nil
}

// Source type is an audit category, never a caller's raw identifier or URL.
func reviewSourceType(source string) string {
	switch source {
	case "human_submission", "digital_share", "github_issue", "twitter", "x", "bluesky":
		return source
	default:
		return "unknown"
	}
}
