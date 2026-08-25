package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluatePassesThresholds(t *testing.T) {
	result := decodeResult(t, `{
		"task":{"status":2,"total":10,"finished":10,"duration_ms":1200},
		"metric":{"retrieval_metrics":{"recall":0.8}},
		"usage":{"cache_hit_rate":0.4,"cost_by_currency":{"USD":0.05}}
	}`)
	report := evaluate(result, baseline{
		RequireSuccess: true, RequireComplete: true,
		Minimum: map[string]float64{"metric.retrieval_metrics.recall": 0.75},
		Maximum: map[string]float64{"task.duration_ms": 2000, "usage.cost_by_currency.USD": 0.1},
	})
	require.True(t, report.Passed)
	require.Len(t, report.Checks, 5)
}

func TestEvaluateReportsMissingAndRegression(t *testing.T) {
	result := decodeResult(t, `{
		"task":{"status":3,"total":10,"finished":8},
		"metric":{"retrieval_metrics":{"recall":0.4}}
	}`)
	report := evaluate(result, baseline{
		RequireSuccess: true, RequireComplete: true,
		Minimum: map[string]float64{"metric.retrieval_metrics.recall": 0.75},
		Maximum: map[string]float64{"usage.cost_by_currency.USD": 0.1},
	})
	require.False(t, report.Passed)
	require.False(t, report.Checks[0].Passed)
	require.False(t, report.Checks[len(report.Checks)-1].Passed)
	require.NotEmpty(t, report.Checks[len(report.Checks)-1].Error)
}

func TestUnwrapAPIData(t *testing.T) {
	result := decodeResult(t, `{"success":true,"data":{"task":{"status":"success"}}}`)
	unwrapped := unwrapAPIData(result)
	status, ok := lookup(unwrapped, "task.status")
	require.True(t, ok)
	require.Equal(t, "success", status)
}

func decodeResult(t *testing.T, raw string) map[string]any {
	t.Helper()
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &result))
	return result
}
