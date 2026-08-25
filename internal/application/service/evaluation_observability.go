package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

type evaluationModelCallRecord struct {
	ID                        string `gorm:"primaryKey;size:36"`
	TaskID                    string `gorm:"size:255;not null;index"`
	TenantID                  uint64 `gorm:"not null;index"`
	ModelID                   string `gorm:"size:255"`
	ModelName                 string `gorm:"size:255;not null"`
	Purpose                   string `gorm:"size:100"`
	PromptPrefixFingerprint   string `gorm:"size:128"`
	PromptTokens              int
	CompletionTokens          int
	TotalTokens               int
	CachedTokens              int
	CacheReadTokens           int
	CacheWriteTokens          int
	CacheMissTokens           int
	CacheReported             bool
	CacheStatus               types.PromptCacheStatus `gorm:"size:32"`
	PricingConfigured         bool
	Currency                  string `gorm:"size:16"`
	InputPricePerMillion      float64
	OutputPricePerMillion     float64
	CacheReadPricePerMillion  float64
	CacheWritePricePerMillion float64
	EstimatedCost             float64
	DurationMS                int64
	Success                   bool   `gorm:"not null"`
	ErrMsg                    string `gorm:"type:text"`
	CreatedAt                 time.Time
}

func (evaluationModelCallRecord) TableName() string {
	return "evaluation_model_calls"
}

type evaluationCallObserver struct {
	ctx      context.Context
	storage  *evaluationStorage
	taskID   string
	tenantID uint64
}

func (o *evaluationCallObserver) ObserveLLMCall(observation types.LLMCallObservation) {
	if err := o.storage.recordModelCall(o.ctx, o.taskID, o.tenantID, observation); err != nil {
		logger.Errorf(o.ctx, "Failed to persist evaluation model call: %v", err)
	}
}

func (e *evaluationStorage) recordModelCall(
	ctx context.Context,
	taskID string,
	tenantID uint64,
	observation types.LLMCallObservation,
) error {
	usage := observation.Usage
	pricing := observation.Pricing.Normalize()
	record := &evaluationModelCallRecord{
		ID: uuid.NewString(), TaskID: taskID, TenantID: tenantID,
		ModelID: observation.ModelID, ModelName: observation.ModelName,
		Purpose: observation.Purpose, PromptPrefixFingerprint: observation.PromptPrefixFingerprint,
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		TotalTokens: usage.TotalTokens, CachedTokens: usage.CachedTokens,
		CacheReadTokens: usage.CacheReadTokens, CacheWriteTokens: usage.CacheWriteTokens,
		CacheMissTokens: usage.CacheMissTokens, CacheReported: usage.CacheReported,
		CacheStatus: usage.CacheStatus, PricingConfigured: pricing.Enabled,
		Currency: pricing.Currency, InputPricePerMillion: pricing.InputPerMillion,
		OutputPricePerMillion:     pricing.OutputPerMillion,
		CacheReadPricePerMillion:  pricing.CacheReadPerMillion,
		CacheWritePricePerMillion: pricing.CacheWritePerMillion,
		EstimatedCost:             observation.EstimatedCost, DurationMS: observation.DurationMS,
		Success: observation.Success, ErrMsg: observation.Error, CreatedAt: time.Now().UTC(),
	}
	return e.db.WithContext(ctx).Create(record).Error
}

func (e *evaluationStorage) getModelCalls(
	ctx context.Context,
	taskID string,
) ([]types.EvaluationModelCall, *types.EvaluationUsage, error) {
	var records []evaluationModelCallRecord
	if err := e.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("created_at ASC, id ASC").
		Find(&records).Error; err != nil {
		return nil, nil, err
	}

	calls := make([]types.EvaluationModelCall, 0, len(records))
	usage := &types.EvaluationUsage{CostByCurrency: make(map[string]float64)}
	for _, record := range records {
		callUsage := types.TokenUsage{
			PromptTokens: record.PromptTokens, CompletionTokens: record.CompletionTokens,
			TotalTokens: record.TotalTokens, CachedTokens: record.CachedTokens,
			CacheReadTokens: record.CacheReadTokens, CacheWriteTokens: record.CacheWriteTokens,
			CacheMissTokens: record.CacheMissTokens, CacheReported: record.CacheReported,
			CacheStatus: record.CacheStatus,
		}
		calls = append(calls, types.EvaluationModelCall{
			ID: record.ID, ModelID: record.ModelID, ModelName: record.ModelName,
			Purpose: record.Purpose, PromptPrefixFingerprint: record.PromptPrefixFingerprint,
			Usage: callUsage,
			Pricing: types.LLMTokenPricing{
				Enabled: record.PricingConfigured, Currency: record.Currency,
				InputPerMillion:      record.InputPricePerMillion,
				OutputPerMillion:     record.OutputPricePerMillion,
				CacheReadPerMillion:  record.CacheReadPricePerMillion,
				CacheWritePerMillion: record.CacheWritePricePerMillion,
			},
			EstimatedCost: record.EstimatedCost,
			DurationMS:    record.DurationMS, Success: record.Success,
			Error: record.ErrMsg, CreatedAt: record.CreatedAt,
		})
		accumulateEvaluationUsage(usage, record)
	}
	finalizeEvaluationUsage(usage)
	return calls, usage, nil
}

func accumulateEvaluationUsage(usage *types.EvaluationUsage, call evaluationModelCallRecord) {
	usage.CallCount++
	if call.Success {
		usage.SuccessfulCalls++
	} else {
		usage.FailedCalls++
	}
	usage.PromptTokens += call.PromptTokens
	usage.CompletionTokens += call.CompletionTokens
	usage.TotalTokens += call.TotalTokens
	usage.CacheReadTokens += call.CacheReadTokens
	usage.CacheWriteTokens += call.CacheWriteTokens
	usage.CacheMissTokens += call.CacheMissTokens
	usage.ModelDurationMS += call.DurationMS
	if call.PricingConfigured {
		usage.PricedCalls++
		usage.CostByCurrency[call.Currency] += call.EstimatedCost
	} else {
		usage.UnpricedCalls++
	}
	if call.CacheReported {
		usage.CacheReportedCalls++
		if call.CacheReadTokens > 0 {
			usage.CacheHitCalls++
		}
	}
}

func finalizeEvaluationUsage(usage *types.EvaluationUsage) {
	if usage.CallCount > 0 {
		usage.AverageModelLatencyMS = float64(usage.ModelDurationMS) / float64(usage.CallCount)
	}
	reportedPromptTokens := usage.CacheReadTokens + usage.CacheMissTokens
	if reportedPromptTokens > 0 {
		usage.CacheHitRate = float64(usage.CacheReadTokens) / float64(reportedPromptTokens)
	}
}
