# Evaluation regression gate

This directory contains the deterministic quality, latency, and cost gate used
by pull requests and the scheduled CI job.

## Run locally

```bash
go test ./internal/application/service/metric ./cmd/evalgate
go run ./cmd/evalgate \
  -result evaluation/fixtures/regression_result.json \
  -baseline evaluation/baseline.json \
  -report evaluation-report.json
```

The result can be either the evaluation API response envelope or its `data`
object. Dot-separated paths in `baseline.json` address numeric result fields.
The command exits with status 1 when any threshold fails and status 2 when its
input or configuration is invalid.

## Adjust the baseline

- `require_success` requires the task status to be successful.
- `require_complete` requires `task.finished` to equal a positive `task.total`.
- `minimum` contains quality metrics that must not fall below their thresholds.
- `maximum` contains latency and cost metrics that must not exceed their limits.

The checked-in fixture makes the CI gate deterministic. Running evaluations
against a deployed WeKnora instance is intentionally not configured here: the
deployment URL, API token, dataset, and result-retention policy must be approved
before CI is allowed to send credentials or evaluation data to another system.
