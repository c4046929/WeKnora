package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareRuns(t *testing.T) {
	before := runInput{
		Embedding: embeddingRun{RequestedTexts: 100, ProviderCalls: 100},
		ModelCalls: []modelCall{
			{Purpose: "wiki_page_modify", Usage: usage{PromptTokens: 100, CacheMissTokens: 100, CacheReported: true}, Pricing: pricing{Currency: "usd"}, EstimatedCost: 0.2},
			{Purpose: "knowledge_qa", Usage: usage{PromptTokens: 1000}},
		},
	}
	after := runInput{
		Embedding: embeddingRun{RequestedTexts: 100, ProviderCalls: 25},
		ModelCalls: []modelCall{
			{Purpose: "wiki_page_modify", Usage: usage{PromptTokens: 100, CacheReadTokens: 80, CacheMissTokens: 20, CacheReported: true}, Pricing: pricing{Currency: "USD"}, EstimatedCost: 0.08},
		},
	}

	report := compareRuns(before, after, "wiki_")
	require.Equal(t, 75, report.Embedding.CallsSaved)
	require.InDelta(t, 0.75, report.Embedding.ReductionRate, 0.0001)
	require.Equal(t, 1, report.Wiki.Before.CallCount)
	require.InDelta(t, 0.8, report.Wiki.After.CacheHitRate, 0.0001)
	require.InDelta(t, 0.8, report.Wiki.CacheHitRateDelta, 0.0001)
	require.InDelta(t, 0.08, report.Wiki.After.CostByCurrency["USD"], 0.0001)
}

func TestSummarizeCallsUsesCallsFallback(t *testing.T) {
	input := runInput{Calls: []modelCall{{
		Purpose: "wiki_summary",
		Usage:   usage{CacheReadTokens: 30, CacheMissTokens: 70, CacheReported: true},
	}}}
	summary := summarizeCalls(allCalls(input), "wiki_")
	require.Equal(t, 1, summary.CallCount)
	require.InDelta(t, 0.3, summary.CacheHitRate, 0.0001)
}
