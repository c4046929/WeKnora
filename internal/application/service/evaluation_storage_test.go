package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEvaluationStoragePersistsAcrossInstances(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "evaluation.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&evaluationRecord{}))

	ctx := context.Background()
	startedAt := time.Now().UTC().Truncate(time.Millisecond)
	params := &types.ChatManage{}
	params.Query = "What is WeKnora?"
	detail := &types.EvaluationDetail{
		Task: &types.EvaluationTask{
			ID: "evaluation-test", TenantID: 7, DatasetID: "dataset-1",
			StartTime: startedAt, Status: types.EvaluationStatuePending,
		},
		Params: params,
	}

	require.NoError(t, newEvaluationStorage(db).register(ctx, detail))

	// A fresh storage instance simulates reading the task after a service restart.
	restartedStorage := newEvaluationStorage(db)
	loaded, err := restartedStorage.get(ctx, detail.Task.ID)
	require.NoError(t, err)
	require.Equal(t, detail.Task.ID, loaded.Task.ID)
	require.Equal(t, detail.Task.TenantID, loaded.Task.TenantID)
	require.Equal(t, detail.Params.Query, loaded.Params.Query)
	require.Nil(t, loaded.Metric)

	finishedAt := startedAt.Add(1250 * time.Millisecond)
	require.NoError(t, restartedStorage.update(ctx, detail.Task.ID, func(current *types.EvaluationDetail) {
		current.Task.Status = types.EvaluationStatueSuccess
		current.Task.EndTime = &finishedAt
		current.Task.DurationMS = 1250
		current.Task.Total = 1
		current.Task.Finished = 1
		current.Metric = &types.MetricResult{
			RetrievalMetrics: types.RetrievalMetrics{Recall: 1},
		}
	}))

	loaded, err = newEvaluationStorage(db).get(ctx, detail.Task.ID)
	require.NoError(t, err)
	require.Equal(t, types.EvaluationStatueSuccess, loaded.Task.Status)
	require.EqualValues(t, 1250, loaded.Task.DurationMS)
	require.Equal(t, 1.0, loaded.Metric.RetrievalMetrics.Recall)
}

func TestEvaluationStorageReturnsTaskNotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "evaluation.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&evaluationRecord{}))

	_, err = newEvaluationStorage(db).get(context.Background(), "missing")
	require.EqualError(t, err, "task not found")
}
