package feedback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	QualityBucketHour       = "hour"
	QualityBucketDay        = "day"
	QualityValueConfigured  = "configured"
	QualityValueOffList     = "off_list"
	QualityValueUnknownDim  = "unknown_dimension"
	QualityValueAll         = "all"
	InvalidDimensionName    = "__invalid_dimension__"
	qualitySampleCap        = 5
	qualityValueDisplayCap  = 160
	qualityDefaultThreshold = 0.60
)

var qualityDimensionNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)

type ClassificationQualityRefreshOpts struct {
	TenantID               string
	From                   time.Time
	To                     time.Time
	BucketWidth            string
	LowConfidenceThreshold float64
}

type ClassificationQualityQueryOpts struct {
	TenantID      string
	From          time.Time
	To            time.Time
	BucketWidth   string
	Source        string
	LogicalModel  string
	ProviderModel string
	ChannelID     string
}

type ClassificationQualitySignalAggregate struct {
	ClassificationEventCount         int64
	FailedAttemptCount               int64
	ParseFailureCount                int64
	TerminalFailureCount             int64
	TerminalParseFailureCount        int64
	OffListCount                     int64
	UnknownDimensionCount            int64
	ConfidenceCount                  int64
	ConfidenceSum                    float64
	LowConfidenceCount               int64
	SampleFeedbackIDs                []int64
	LowConfidenceSampleFeedbackIDs   []int64
	OffListSampleFeedbackIDs         []int64
	ParseFailureSampleFeedbackIDs    []int64
	TerminalFailureSampleFeedbackIDs []int64
}

type ClassificationQualityValueAggregate struct {
	DimensionName         string
	DimensionValueHash    string
	DimensionValueDisplay string
	ValueStatus           string
	AppearanceCount       int64
	EventCount            int64
	ConfidenceCount       int64
	ConfidenceSum         float64
	LowConfidenceCount    int64
	SampleFeedbackIDs     []int64
}

type ClassificationQualitySeriesBucket struct {
	Bucket                   time.Time
	ClassificationEventCount int64
	FailedAttemptCount       int64
	ParseFailureCount        int64
	TerminalFailureCount     int64
	OffListCount             int64
	UnknownDimensionCount    int64
	ConfidenceCount          int64
	ConfidenceSum            float64
	LowConfidenceCount       int64
}

type ClassificationQualitySample struct {
	ID                       int64
	CreatedAt                time.Time
	EnrichedAt               *time.Time
	Source                   string
	Title                    string
	DisplayTitle             string
	EnrichmentStatus         string
	ClassificationConfidence *float64
}

type qualityAccumulator struct {
	signals      map[qualitySignalKey]*ClassificationQualitySignalAggregate
	values       map[qualityValueKey]*ClassificationQualityValueAggregate
	maxRunID     int64
	maxFailureID int64
}

type qualitySignalKey struct {
	BucketStart   time.Time
	BucketWidth   string
	Source        string
	LogicalModel  string
	ProviderModel string
	ChannelID     string
}

type qualityValueKey struct {
	qualitySignalKey
	DimensionName      string
	DimensionValueHash string
	ValueStatus        string
}

type semanticQualityRow struct {
	ID            int64
	EventAt       time.Time
	FeedbackID    int64
	Source        string
	LogicalModel  string
	ProviderModel string
	ChannelID     string
	Attrs         []byte
	DroppedAttrs  []byte
	Confidence    []byte
}

type failureQualityRow struct {
	ID            int64
	EventAt       time.Time
	FeedbackID    int64
	Source        string
	LogicalModel  string
	ProviderModel string
	ChannelID     string
	ReasonClass   string
	Terminal      bool
}

type droppedAttrsPayload struct {
	Diagnostics []domain.AttrDropDiagnostic `json:"diagnostics"`
}

func (r *FeedbackRepo) RefreshClassificationQuality(
	ctx context.Context,
	opts ClassificationQualityRefreshOpts,
) error {
	opts = normalizeQualityRefreshOpts(opts)
	acc := ptrext.Of(qualityAccumulator{
		signals: make(map[qualitySignalKey]*ClassificationQualitySignalAggregate),
		values:  make(map[qualityValueKey]*ClassificationQualityValueAggregate),
	})
	if err := r.consumeSemanticQualityRows(ctx, opts, acc); err != nil {
		return err
	}
	if err := r.consumeFailureQualityRows(ctx, opts, acc); err != nil {
		return err
	}
	return r.replaceQualityBuckets(ctx, opts, ptrext.Indirect(acc))
}

func normalizeQualityRefreshOpts(opts ClassificationQualityRefreshOpts) ClassificationQualityRefreshOpts {
	opts.From = opts.From.UTC()
	opts.To = opts.To.UTC()
	if opts.BucketWidth != QualityBucketHour {
		opts.BucketWidth = QualityBucketDay
	}
	if opts.LowConfidenceThreshold <= 0 || opts.LowConfidenceThreshold > 1 {
		opts.LowConfidenceThreshold = qualityDefaultThreshold
	}
	return opts
}

func normalizeQualityQueryOpts(opts ClassificationQualityQueryOpts) ClassificationQualityQueryOpts {
	opts.From = opts.From.UTC()
	opts.To = opts.To.UTC()
	if opts.BucketWidth != QualityBucketHour {
		opts.BucketWidth = QualityBucketDay
	}
	opts.From, opts.To = qualityBucketRange(opts.From, opts.To, opts.BucketWidth)
	return opts
}

func (r *FeedbackRepo) consumeSemanticQualityRows(
	ctx context.Context,
	opts ClassificationQualityRefreshOpts,
	acc *qualityAccumulator,
) error {
	rows, err := r.pool.Query(ctx, `
		SELECT ser.id,
		       ser.created_at,
		       ser.subject_id,
		       COALESCE(NULLIF(ser.source, ''), uf.source, ''),
		       COALESCE(NULLIF(ser.logical_model, ''), NULLIF(ser.model, ''), ''),
		       COALESCE(NULLIF(ser.provider_model, ''), ''),
		       COALESCE(NULLIF(ser.channel_id, ''), ''),
		       ser.attrs,
		       ser.dropped_attrs,
		       ser.confidence
		  FROM semantic_extraction_runs ser
		  LEFT JOIN user_feedback uf
		    ON uf.id = ser.subject_id
		   AND uf.tenant_id = ser.tenant_id
		   AND uf.deleted_at IS NULL
		 WHERE ser.tenant_id = $1
		   AND ser.subject_type = $2
		   AND ser.created_at >= $3
		   AND ser.created_at < $4
		 ORDER BY ser.created_at DESC, ser.id DESC`,
		opts.TenantID, SemanticSubjectFeedback, opts.From, opts.To,
	)
	if err != nil {
		return fmt.Errorf("classification quality semantic rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row semanticQualityRow
		if err := rows.Scan(
			&row.ID, &row.EventAt, &row.FeedbackID, &row.Source,
			&row.LogicalModel, &row.ProviderModel, &row.ChannelID,
			&row.Attrs, &row.DroppedAttrs, &row.Confidence,
		); err != nil {
			return fmt.Errorf("scan classification quality semantic row: %w", err)
		}
		acc.consumeSemantic(opts, row)
	}
	return rows.Err()
}

func (r *FeedbackRepo) consumeFailureQualityRows(
	ctx context.Context,
	opts ClassificationQualityRefreshOpts,
	acc *qualityAccumulator,
) error {
	rows, err := r.pool.Query(ctx, `
		SELECT id,
		       event_at,
		       COALESCE(feedback_id, 0),
		       source,
		       logical_model,
		       provider_model,
		       channel_id,
		       reason_class,
		       terminal
		  FROM classification_quality_failure_events
		 WHERE tenant_id = $1
		   AND event_at >= $2
		   AND event_at < $3
		 ORDER BY event_at DESC, id DESC`,
		opts.TenantID, opts.From, opts.To,
	)
	if err != nil {
		return fmt.Errorf("classification quality failure rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row failureQualityRow
		if err := rows.Scan(
			&row.ID, &row.EventAt, &row.FeedbackID, &row.Source,
			&row.LogicalModel, &row.ProviderModel, &row.ChannelID,
			&row.ReasonClass, &row.Terminal,
		); err != nil {
			return fmt.Errorf("scan classification quality failure row: %w", err)
		}
		acc.consumeFailure(opts, row)
	}
	return rows.Err()
}

func (acc *qualityAccumulator) consumeSemantic(opts ClassificationQualityRefreshOpts, row semanticQualityRow) {
	if row.ID > acc.maxRunID {
		acc.maxRunID = row.ID
	}
	confidence, hasConfidence := confidenceFromAudit(row.Confidence)
	signal := acc.signal(keyFromEvent(opts, row.EventAt, row.Source, row.LogicalModel, row.ProviderModel, row.ChannelID))
	signal.ClassificationEventCount++
	signal.SampleFeedbackIDs = addSample(signal.SampleFeedbackIDs, row.FeedbackID)
	addSignalConfidence(signal, confidence, hasConfidence, opts.LowConfidenceThreshold)
	if hasConfidence && confidence <= opts.LowConfidenceThreshold {
		signal.LowConfidenceSampleFeedbackIDs = addSample(signal.LowConfidenceSampleFeedbackIDs, row.FeedbackID)
	}
	attrs := decodeAttrs(row.Attrs)
	for dim, raw := range attrs {
		values := attrValues(raw)
		if len(values) == 0 {
			continue
		}
		acc.addDimensionAll(opts, row, dim, confidence, hasConfidence)
		seenEventValues := map[string]bool{}
		for _, value := range values {
			acc.addValue(opts, row, dim, value, QualityValueConfigured, confidence, hasConfidence, seenEventValues)
		}
	}
	for _, diag := range droppedDiagnostics(row.DroppedAttrs) {
		acc.consumeDiagnostic(opts, row, diag, signal, confidence, hasConfidence)
	}
}

func (acc *qualityAccumulator) consumeFailure(opts ClassificationQualityRefreshOpts, row failureQualityRow) {
	if row.ID > acc.maxFailureID {
		acc.maxFailureID = row.ID
	}
	signal := acc.signal(keyFromEvent(opts, row.EventAt, row.Source, row.LogicalModel, row.ProviderModel, row.ChannelID))
	signal.FailedAttemptCount++
	if row.ReasonClass == "parse_err" {
		signal.ParseFailureCount++
		signal.ParseFailureSampleFeedbackIDs = addSample(signal.ParseFailureSampleFeedbackIDs, row.FeedbackID)
	}
	if row.Terminal {
		signal.TerminalFailureCount++
		signal.TerminalFailureSampleFeedbackIDs = addSample(signal.TerminalFailureSampleFeedbackIDs, row.FeedbackID)
		if row.ReasonClass == "parse_err" {
			signal.TerminalParseFailureCount++
		}
	}
	signal.SampleFeedbackIDs = addSample(signal.SampleFeedbackIDs, row.FeedbackID)
}

func (acc *qualityAccumulator) consumeDiagnostic(
	opts ClassificationQualityRefreshOpts,
	row semanticQualityRow,
	diag domain.AttrDropDiagnostic,
	signal *ClassificationQualitySignalAggregate,
	confidence float64,
	hasConfidence bool,
) {
	count := qualityDiagnosticCount(diag)
	if count == 0 {
		return
	}
	switch diag.Reason {
	case domain.AttrDropOffListValue:
		signal.OffListCount += count
		signal.OffListSampleFeedbackIDs = addSample(signal.OffListSampleFeedbackIDs, row.FeedbackID)
		acc.addDiagnosticValues(opts, row, diag, QualityValueOffList, confidence, hasConfidence)
	case domain.AttrDropUnknownDimension:
		signal.UnknownDimensionCount += count
		acc.addDiagnosticValues(opts, row, diag, QualityValueUnknownDim, confidence, hasConfidence)
	}
}

func qualityDiagnosticCount(diag domain.AttrDropDiagnostic) int64 {
	if diag.Count <= 0 {
		return 0
	}
	return int64(diag.Count)
}

func (acc *qualityAccumulator) addDiagnosticValues(
	opts ClassificationQualityRefreshOpts,
	row semanticQualityRow,
	diag domain.AttrDropDiagnostic,
	status string,
	confidence float64,
	hasConfidence bool,
) {
	dim := normalizeQualityDimensionName(diag.Dim)
	seenEventValues := map[string]bool{}
	for _, value := range diag.Values {
		acc.addValue(opts, row, dim, value, status, confidence, hasConfidence, seenEventValues)
	}
}

func (acc *qualityAccumulator) addDimensionAll(
	opts ClassificationQualityRefreshOpts,
	row semanticQualityRow,
	dim string,
	confidence float64,
	hasConfidence bool,
) {
	key := valueKeyFromEvent(opts, row, normalizeQualityDimensionName(dim), "", QualityValueAll)
	agg := acc.value(key)
	agg.EventCount++
	agg.SampleFeedbackIDs = addSample(agg.SampleFeedbackIDs, row.FeedbackID)
	addValueConfidence(agg, confidence, hasConfidence, opts.LowConfidenceThreshold)
}

func (acc *qualityAccumulator) addValue(
	opts ClassificationQualityRefreshOpts,
	row semanticQualityRow,
	dim string,
	value string,
	status string,
	confidence float64,
	hasConfidence bool,
	seenEventValues map[string]bool,
) {
	display, hash := normalizeQualityValue(value)
	if hash == "" {
		return
	}
	key := valueKeyFromEvent(opts, row, normalizeQualityDimensionName(dim), hash, status)
	agg := acc.value(key)
	if agg.DimensionValueDisplay == "" {
		agg.DimensionValueDisplay = display
	}
	agg.AppearanceCount++
	if !seenEventValues[hash] {
		agg.EventCount++
		seenEventValues[hash] = true
		addValueConfidence(agg, confidence, hasConfidence, opts.LowConfidenceThreshold)
	}
	agg.SampleFeedbackIDs = addSample(agg.SampleFeedbackIDs, row.FeedbackID)
}

func (acc *qualityAccumulator) signal(key qualitySignalKey) *ClassificationQualitySignalAggregate {
	if agg, ok := acc.signals[key]; ok {
		return agg
	}
	agg := ptrext.Of(ClassificationQualitySignalAggregate{})
	acc.signals[key] = agg
	return agg
}

func (acc *qualityAccumulator) value(key qualityValueKey) *ClassificationQualityValueAggregate {
	if agg, ok := acc.values[key]; ok {
		return agg
	}
	agg := ptrext.Of(ClassificationQualityValueAggregate{
		DimensionName:      key.DimensionName,
		DimensionValueHash: key.DimensionValueHash,
		ValueStatus:        key.ValueStatus,
	})
	acc.values[key] = agg
	return agg
}

func keyFromEvent(
	opts ClassificationQualityRefreshOpts,
	eventAt time.Time,
	source string,
	logicalModel string,
	providerModel string,
	channelID string,
) qualitySignalKey {
	return qualitySignalKey{
		BucketStart:   bucketStart(eventAt, opts.BucketWidth),
		BucketWidth:   opts.BucketWidth,
		Source:        strings.TrimSpace(source),
		LogicalModel:  strings.TrimSpace(logicalModel),
		ProviderModel: strings.TrimSpace(providerModel),
		ChannelID:     strings.TrimSpace(channelID),
	}
}

func valueKeyFromEvent(
	opts ClassificationQualityRefreshOpts,
	row semanticQualityRow,
	dim string,
	valueHash string,
	status string,
) qualityValueKey {
	return qualityValueKey{
		qualitySignalKey:   keyFromEvent(opts, row.EventAt, row.Source, row.LogicalModel, row.ProviderModel, row.ChannelID),
		DimensionName:      dim,
		DimensionValueHash: valueHash,
		ValueStatus:        status,
	}
}

func bucketStart(t time.Time, width string) time.Time {
	t = t.UTC()
	if width == QualityBucketHour {
		return t.Truncate(time.Hour)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func qualityBucketRange(from time.Time, to time.Time, width string) (time.Time, time.Time) {
	start := bucketStart(from, width)
	end := bucketStart(to, width)
	if to.After(end) {
		end = addQualityBucket(end, width)
	}
	return start, end
}

func addQualityBucket(t time.Time, width string) time.Time {
	if width == QualityBucketHour {
		return t.Add(time.Hour)
	}
	return t.AddDate(0, 0, 1)
}

func decodeAttrs(raw []byte) map[string]any {
	var attrs map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &attrs) != nil {
		return nil
	}
	return attrs
}

func droppedDiagnostics(raw []byte) []domain.AttrDropDiagnostic {
	var payload droppedAttrsPayload
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return payload.Diagnostics
}

func attrValues(raw any) []string {
	switch v := raw.(type) {
	case string:
		return nonEmptyStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, nonEmptyStrings(s)...)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			out = append(out, nonEmptyStrings(s)...)
		}
		return out
	default:
		return nil
	}
}

func nonEmptyStrings(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}

func confidenceFromAudit(raw []byte) (float64, bool) {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return 0, false
	}
	switch v := payload["overall"].(type) {
	case float64:
		return v, v >= 0 && v <= 1
	case int:
		f := float64(v)
		return f, f >= 0 && f <= 1
	default:
		return 0, false
	}
}

func addSignalConfidence(agg *ClassificationQualitySignalAggregate, confidence float64, ok bool, threshold float64) {
	if !ok {
		return
	}
	agg.ConfidenceCount++
	agg.ConfidenceSum += confidence
	if confidence <= threshold {
		agg.LowConfidenceCount++
	}
}

func addValueConfidence(agg *ClassificationQualityValueAggregate, confidence float64, ok bool, threshold float64) {
	if !ok {
		return
	}
	agg.ConfidenceCount++
	agg.ConfidenceSum += confidence
	if confidence <= threshold {
		agg.LowConfidenceCount++
	}
}

func addSample(samples []int64, id int64) []int64 {
	if id <= 0 || len(samples) >= qualitySampleCap {
		return samples
	}
	for _, existing := range samples {
		if existing == id {
			return samples
		}
	}
	return append(samples, id)
}

func normalizeQualityDimensionName(name string) string {
	name = strings.TrimSpace(name)
	if qualityDimensionNameRe.MatchString(name) {
		return name
	}
	return InvalidDimensionName
}

func normalizeQualityValue(value string) (display string, hash string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	sum := sha256.Sum256([]byte(value))
	return capUTF8Bytes(strings.ToValidUTF8(value, "\uFFFD"), qualityValueDisplayCap), hex.EncodeToString(sum[:])
}

func capUTF8Bytes(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	var b strings.Builder
	for _, r := range value {
		if b.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (r *FeedbackRepo) replaceQualityBuckets(
	ctx context.Context,
	opts ClassificationQualityRefreshOpts,
	acc qualityAccumulator,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin quality refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockQualityRefresh(ctx, tx, opts); err != nil {
		return err
	}
	if err := deleteQualityBuckets(ctx, tx, opts); err != nil {
		return err
	}
	if err := insertQualitySignals(ctx, tx, opts, acc.signals); err != nil {
		return err
	}
	if err := insertQualityValues(ctx, tx, opts, acc.values); err != nil {
		return err
	}
	if err := upsertQualityState(ctx, tx, opts, acc); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit quality refresh: %w", err)
	}
	return nil
}

func lockQualityRefresh(ctx context.Context, tx pgx.Tx, opts ClassificationQualityRefreshOpts) error {
	lockKey := "classification_quality:" + opts.TenantID + ":" + opts.BucketWidth
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("lock quality refresh: %w", err)
	}
	return nil
}

func deleteQualityBuckets(ctx context.Context, tx pgx.Tx, opts ClassificationQualityRefreshOpts) error {
	bucketFrom, bucketTo := qualityBucketRange(opts.From, opts.To, opts.BucketWidth)
	for _, table := range []string{"classification_quality_value_buckets", "classification_quality_signal_buckets"} {
		_, err := tx.Exec(ctx, fmt.Sprintf(`
			DELETE FROM %s
			 WHERE tenant_id = $1
			   AND bucket_width = $2
			   AND bucket_start >= $3
			   AND bucket_start < $4`, table),
			opts.TenantID, opts.BucketWidth, bucketFrom, bucketTo,
		)
		if err != nil {
			return fmt.Errorf("delete %s: %w", table, err)
		}
	}
	return nil
}

func insertQualitySignals(
	ctx context.Context,
	tx pgx.Tx,
	opts ClassificationQualityRefreshOpts,
	signals map[qualitySignalKey]*ClassificationQualitySignalAggregate,
) error {
	keys := make([]qualitySignalKey, 0, len(signals))
	for key := range signals {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].BucketStart.Before(keys[j].BucketStart) })
	for _, key := range keys {
		agg := signals[key]
		if _, err := tx.Exec(ctx, `
			INSERT INTO classification_quality_signal_buckets
			 (tenant_id, bucket_start, bucket_width, source, logical_model, provider_model, channel_id,
			  classification_event_count, failed_attempt_count, parse_failure_count,
			  terminal_failure_count, terminal_parse_failure_count, off_list_count,
			  unknown_dimension_count, confidence_count, confidence_sum, low_confidence_count,
			  sample_feedback_ids, low_confidence_sample_feedback_ids, off_list_sample_feedback_ids,
			  parse_failure_sample_feedback_ids, terminal_failure_sample_feedback_ids, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			        $15, $16, $17, $18, $19, $20, $21, $22, NOW())
			ON CONFLICT (
			  tenant_id, bucket_width, bucket_start, source, logical_model, provider_model, channel_id
			) DO UPDATE SET
			  classification_event_count = EXCLUDED.classification_event_count,
			  failed_attempt_count = EXCLUDED.failed_attempt_count,
			  parse_failure_count = EXCLUDED.parse_failure_count,
			  terminal_failure_count = EXCLUDED.terminal_failure_count,
			  terminal_parse_failure_count = EXCLUDED.terminal_parse_failure_count,
			  off_list_count = EXCLUDED.off_list_count,
			  unknown_dimension_count = EXCLUDED.unknown_dimension_count,
			  confidence_count = EXCLUDED.confidence_count,
			  confidence_sum = EXCLUDED.confidence_sum,
			  low_confidence_count = EXCLUDED.low_confidence_count,
			  sample_feedback_ids = EXCLUDED.sample_feedback_ids,
			  low_confidence_sample_feedback_ids = EXCLUDED.low_confidence_sample_feedback_ids,
			  off_list_sample_feedback_ids = EXCLUDED.off_list_sample_feedback_ids,
			  parse_failure_sample_feedback_ids = EXCLUDED.parse_failure_sample_feedback_ids,
			  terminal_failure_sample_feedback_ids = EXCLUDED.terminal_failure_sample_feedback_ids,
			  updated_at = NOW()`,
			opts.TenantID, key.BucketStart, key.BucketWidth, key.Source, key.LogicalModel,
			key.ProviderModel, key.ChannelID, agg.ClassificationEventCount,
			agg.FailedAttemptCount, agg.ParseFailureCount, agg.TerminalFailureCount,
			agg.TerminalParseFailureCount, agg.OffListCount, agg.UnknownDimensionCount,
			agg.ConfidenceCount, agg.ConfidenceSum, agg.LowConfidenceCount, qualitySamplesForSQL(agg.SampleFeedbackIDs),
			qualitySamplesForSQL(agg.LowConfidenceSampleFeedbackIDs), qualitySamplesForSQL(agg.OffListSampleFeedbackIDs),
			qualitySamplesForSQL(agg.ParseFailureSampleFeedbackIDs), qualitySamplesForSQL(agg.TerminalFailureSampleFeedbackIDs),
		); err != nil {
			return fmt.Errorf("insert quality signal bucket: %w", err)
		}
	}
	return nil
}

func insertQualityValues(
	ctx context.Context,
	tx pgx.Tx,
	opts ClassificationQualityRefreshOpts,
	values map[qualityValueKey]*ClassificationQualityValueAggregate,
) error {
	keys := make([]qualityValueKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].BucketStart.Before(keys[j].BucketStart) })
	for _, key := range keys {
		agg := values[key]
		if _, err := tx.Exec(ctx, `
			INSERT INTO classification_quality_value_buckets
			 (tenant_id, bucket_start, bucket_width, dimension_name, dimension_value_hash,
			  dimension_value_display, value_status, source, logical_model, provider_model,
			  channel_id, appearance_count, event_count, confidence_count, confidence_sum,
			  low_confidence_count, sample_feedback_ids, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			        $15, $16, $17, NOW())
			ON CONFLICT (
			  tenant_id, bucket_width, bucket_start, dimension_name,
			  dimension_value_hash, value_status, source, logical_model,
			  provider_model, channel_id
			) DO UPDATE SET
			  dimension_value_display = EXCLUDED.dimension_value_display,
			  appearance_count = EXCLUDED.appearance_count,
			  event_count = EXCLUDED.event_count,
			  confidence_count = EXCLUDED.confidence_count,
			  confidence_sum = EXCLUDED.confidence_sum,
			  low_confidence_count = EXCLUDED.low_confidence_count,
			  sample_feedback_ids = EXCLUDED.sample_feedback_ids,
			  updated_at = NOW()`,
			opts.TenantID, key.BucketStart, key.BucketWidth,
			key.DimensionName, key.DimensionValueHash, agg.DimensionValueDisplay, key.ValueStatus,
			key.Source, key.LogicalModel, key.ProviderModel, key.ChannelID, agg.AppearanceCount,
			agg.EventCount, agg.ConfidenceCount, agg.ConfidenceSum, agg.LowConfidenceCount,
			qualitySamplesForSQL(agg.SampleFeedbackIDs),
		); err != nil {
			return fmt.Errorf("insert quality value bucket: %w", err)
		}
	}
	return nil
}

func qualitySamplesForSQL(samples []int64) []int64 {
	if samples == nil {
		return []int64{}
	}
	return samples
}

func upsertQualityState(ctx context.Context, tx pgx.Tx, opts ClassificationQualityRefreshOpts, acc qualityAccumulator) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO classification_quality_rollup_state
		 (tenant_id, bucket_width, last_semantic_run_id, last_failure_event_id,
		  recompute_from, data_through, updated_at)
		VALUES ($1, $2, $3, $4, NULL, $5, NOW())
		ON CONFLICT (tenant_id, bucket_width) DO UPDATE SET
		  last_semantic_run_id = GREATEST(classification_quality_rollup_state.last_semantic_run_id, EXCLUDED.last_semantic_run_id),
		  last_failure_event_id = GREATEST(classification_quality_rollup_state.last_failure_event_id, EXCLUDED.last_failure_event_id),
		  data_through = GREATEST(classification_quality_rollup_state.data_through, EXCLUDED.data_through),
		  updated_at = NOW()`,
		opts.TenantID, opts.BucketWidth, acc.maxRunID, acc.maxFailureID, opts.To,
	)
	if err != nil {
		return fmt.Errorf("upsert quality state: %w", err)
	}
	return nil
}

func (r *FeedbackRepo) ClassificationQualityAggregates(
	ctx context.Context,
	opts ClassificationQualityQueryOpts,
) (ClassificationQualitySignalAggregate, []ClassificationQualityValueAggregate, error) {
	signal, err := r.classificationQualitySignal(ctx, opts)
	if err != nil {
		return ClassificationQualitySignalAggregate{}, nil, err
	}
	values, err := r.classificationQualityValues(ctx, opts)
	if err != nil {
		return ClassificationQualitySignalAggregate{}, nil, err
	}
	return signal, values, nil
}

func (r *FeedbackRepo) classificationQualitySignal(
	ctx context.Context,
	opts ClassificationQualityQueryOpts,
) (ClassificationQualitySignalAggregate, error) {
	opts = normalizeQualityQueryOpts(opts)
	rows, err := r.pool.Query(ctx, `
		SELECT classification_event_count, failed_attempt_count, parse_failure_count,
		       terminal_failure_count, terminal_parse_failure_count, off_list_count,
		       unknown_dimension_count, confidence_count, confidence_sum,
		       low_confidence_count, sample_feedback_ids,
		       low_confidence_sample_feedback_ids, off_list_sample_feedback_ids,
		       parse_failure_sample_feedback_ids, terminal_failure_sample_feedback_ids
		  FROM classification_quality_signal_buckets
		 WHERE tenant_id = $1
		   AND bucket_width = $2
		   AND bucket_start >= $3
		   AND bucket_start < $4
		   AND ($5 = '' OR source = $5)
		   AND ($6 = '' OR logical_model = $6)
		   AND ($7 = '' OR provider_model = $7)
		   AND ($8 = '' OR channel_id = $8)`,
		opts.TenantID, opts.BucketWidth, opts.From, opts.To, opts.Source,
		opts.LogicalModel, opts.ProviderModel, opts.ChannelID,
	)
	if err != nil {
		return ClassificationQualitySignalAggregate{}, fmt.Errorf("quality signal aggregate: %w", err)
	}
	defer rows.Close()
	var out ClassificationQualitySignalAggregate
	for rows.Next() {
		var row ClassificationQualitySignalAggregate
		if err := rows.Scan(
			&row.ClassificationEventCount, &row.FailedAttemptCount, &row.ParseFailureCount,
			&row.TerminalFailureCount, &row.TerminalParseFailureCount, &row.OffListCount,
			&row.UnknownDimensionCount, &row.ConfidenceCount, &row.ConfidenceSum,
			&row.LowConfidenceCount, &row.SampleFeedbackIDs,
			&row.LowConfidenceSampleFeedbackIDs, &row.OffListSampleFeedbackIDs,
			&row.ParseFailureSampleFeedbackIDs, &row.TerminalFailureSampleFeedbackIDs,
		); err != nil {
			return ClassificationQualitySignalAggregate{}, fmt.Errorf("scan quality signal aggregate: %w", err)
		}
		out.mergeSignal(row)
	}
	return out, rows.Err()
}

func (out *ClassificationQualitySignalAggregate) mergeSignal(row ClassificationQualitySignalAggregate) {
	out.ClassificationEventCount += row.ClassificationEventCount
	out.FailedAttemptCount += row.FailedAttemptCount
	out.ParseFailureCount += row.ParseFailureCount
	out.TerminalFailureCount += row.TerminalFailureCount
	out.TerminalParseFailureCount += row.TerminalParseFailureCount
	out.OffListCount += row.OffListCount
	out.UnknownDimensionCount += row.UnknownDimensionCount
	out.ConfidenceCount += row.ConfidenceCount
	out.ConfidenceSum += row.ConfidenceSum
	out.LowConfidenceCount += row.LowConfidenceCount
	out.SampleFeedbackIDs = appendSamples(out.SampleFeedbackIDs, row.SampleFeedbackIDs, 50)
	out.LowConfidenceSampleFeedbackIDs = appendSamples(out.LowConfidenceSampleFeedbackIDs, row.LowConfidenceSampleFeedbackIDs, qualitySampleCap)
	out.OffListSampleFeedbackIDs = appendSamples(out.OffListSampleFeedbackIDs, row.OffListSampleFeedbackIDs, qualitySampleCap)
	out.ParseFailureSampleFeedbackIDs = appendSamples(out.ParseFailureSampleFeedbackIDs, row.ParseFailureSampleFeedbackIDs, qualitySampleCap)
	out.TerminalFailureSampleFeedbackIDs = appendSamples(out.TerminalFailureSampleFeedbackIDs, row.TerminalFailureSampleFeedbackIDs, qualitySampleCap)
}

func (r *FeedbackRepo) classificationQualityValues(
	ctx context.Context,
	opts ClassificationQualityQueryOpts,
) ([]ClassificationQualityValueAggregate, error) {
	opts = normalizeQualityQueryOpts(opts)
	rows, err := r.pool.Query(ctx, `
		SELECT dimension_name, dimension_value_hash, dimension_value_display, value_status,
		       appearance_count, event_count, confidence_count, confidence_sum,
		       low_confidence_count, sample_feedback_ids
		  FROM classification_quality_value_buckets
		 WHERE tenant_id = $1
		   AND bucket_width = $2
		   AND bucket_start >= $3
		   AND bucket_start < $4
		   AND ($5 = '' OR source = $5)
		   AND ($6 = '' OR logical_model = $6)
		   AND ($7 = '' OR provider_model = $7)
		   AND ($8 = '' OR channel_id = $8)`,
		opts.TenantID, opts.BucketWidth, opts.From, opts.To, opts.Source,
		opts.LogicalModel, opts.ProviderModel, opts.ChannelID,
	)
	if err != nil {
		return nil, fmt.Errorf("quality value aggregate: %w", err)
	}
	defer rows.Close()
	merged := map[string]*ClassificationQualityValueAggregate{}
	for rows.Next() {
		var row ClassificationQualityValueAggregate
		if err := rows.Scan(
			&row.DimensionName, &row.DimensionValueHash, &row.DimensionValueDisplay, &row.ValueStatus,
			&row.AppearanceCount, &row.EventCount, &row.ConfidenceCount, &row.ConfidenceSum,
			&row.LowConfidenceCount, &row.SampleFeedbackIDs,
		); err != nil {
			return nil, fmt.Errorf("scan quality value aggregate: %w", err)
		}
		mergeQualityValue(merged, row)
	}
	out := make([]ClassificationQualityValueAggregate, 0, len(merged))
	for _, row := range merged {
		out = append(out, ptrext.Indirect(row))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DimensionName != out[j].DimensionName {
			return out[i].DimensionName < out[j].DimensionName
		}
		return out[i].AppearanceCount > out[j].AppearanceCount
	})
	return out, rows.Err()
}

func mergeQualityValue(merged map[string]*ClassificationQualityValueAggregate, row ClassificationQualityValueAggregate) {
	key := row.DimensionName + "\x00" + row.DimensionValueHash + "\x00" + row.ValueStatus
	dst := merged[key]
	if dst == nil {
		dst = ptrext.Of(ClassificationQualityValueAggregate{
			DimensionName:         row.DimensionName,
			DimensionValueHash:    row.DimensionValueHash,
			DimensionValueDisplay: row.DimensionValueDisplay,
			ValueStatus:           row.ValueStatus,
		})
		merged[key] = dst
	}
	dst.AppearanceCount += row.AppearanceCount
	dst.EventCount += row.EventCount
	dst.ConfidenceCount += row.ConfidenceCount
	dst.ConfidenceSum += row.ConfidenceSum
	dst.LowConfidenceCount += row.LowConfidenceCount
	dst.SampleFeedbackIDs = appendSamples(dst.SampleFeedbackIDs, row.SampleFeedbackIDs, qualitySampleCap)
}

func appendSamples(dst []int64, src []int64, limit int) []int64 {
	for _, id := range src {
		if len(dst) >= limit {
			return dst
		}
		dst = addSample(dst, id)
	}
	return dst
}

func (r *FeedbackRepo) ClassificationQualitySeries(
	ctx context.Context,
	opts ClassificationQualityQueryOpts,
) ([]ClassificationQualitySeriesBucket, error) {
	opts = normalizeQualityQueryOpts(opts)
	rows, err := r.pool.Query(ctx, `
		SELECT bucket_start,
		       COALESCE(SUM(classification_event_count), 0),
		       COALESCE(SUM(failed_attempt_count), 0),
		       COALESCE(SUM(parse_failure_count), 0),
		       COALESCE(SUM(terminal_failure_count), 0),
		       COALESCE(SUM(off_list_count), 0),
		       COALESCE(SUM(unknown_dimension_count), 0),
		       COALESCE(SUM(confidence_count), 0),
		       COALESCE(SUM(confidence_sum), 0)::float8,
		       COALESCE(SUM(low_confidence_count), 0)
		  FROM classification_quality_signal_buckets
		 WHERE tenant_id = $1
		   AND bucket_width = $2
		   AND bucket_start >= $3
		   AND bucket_start < $4
		   AND ($5 = '' OR source = $5)
		   AND ($6 = '' OR logical_model = $6)
		   AND ($7 = '' OR provider_model = $7)
		   AND ($8 = '' OR channel_id = $8)
		 GROUP BY bucket_start
		 ORDER BY bucket_start ASC`,
		opts.TenantID, opts.BucketWidth, opts.From, opts.To, opts.Source,
		opts.LogicalModel, opts.ProviderModel, opts.ChannelID,
	)
	if err != nil {
		return nil, fmt.Errorf("quality series: %w", err)
	}
	defer rows.Close()
	var out []ClassificationQualitySeriesBucket
	for rows.Next() {
		var row ClassificationQualitySeriesBucket
		if err := rows.Scan(
			&row.Bucket, &row.ClassificationEventCount, &row.FailedAttemptCount,
			&row.ParseFailureCount, &row.TerminalFailureCount, &row.OffListCount,
			&row.UnknownDimensionCount, &row.ConfidenceCount, &row.ConfidenceSum,
			&row.LowConfidenceCount,
		); err != nil {
			return nil, fmt.Errorf("scan quality series: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *FeedbackRepo) ClassificationQualitySamples(
	ctx context.Context,
	tenantID string,
	ids []int64,
) ([]ClassificationQualitySample, error) {
	ids = uniquePositiveIDs(ids, 50)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, created_at, enriched_at, source,
		       COALESCE(enriched_title, ''),
		       COALESCE(enriched_display_title, ''),
		       enrichment_status,
		       classification_confidence
		  FROM user_feedback
		 WHERE tenant_id = $1
		   AND id = ANY($2)
		   AND deleted_at IS NULL
		 ORDER BY created_at DESC, id DESC`,
		tenantID, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("quality samples: %w", err)
	}
	defer rows.Close()
	var out []ClassificationQualitySample
	for rows.Next() {
		var row ClassificationQualitySample
		var enrichedAt pgtype.Timestamptz
		var confidence *float64
		if err := rows.Scan(
			&row.ID, &row.CreatedAt, &enrichedAt, &row.Source, &row.Title,
			&row.DisplayTitle, &row.EnrichmentStatus, &confidence,
		); err != nil {
			return nil, fmt.Errorf("scan quality sample: %w", err)
		}
		if enrichedAt.Valid {
			row.EnrichedAt = ptrext.Of(enrichedAt.Time)
		}
		row.ClassificationConfidence = confidence
		out = append(out, row)
	}
	return out, rows.Err()
}

func uniquePositiveIDs(ids []int64, limit int) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= limit {
			break
		}
	}
	return out
}
