package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type baseline struct {
	RequireSuccess  bool               `json:"require_success"`
	RequireComplete bool               `json:"require_complete"`
	Minimum         map[string]float64 `json:"minimum"`
	Maximum         map[string]float64 `json:"maximum"`
}

type check struct {
	Path     string  `json:"path"`
	Rule     string  `json:"rule"`
	Expected float64 `json:"expected"`
	Actual   float64 `json:"actual"`
	Passed   bool    `json:"passed"`
	Error    string  `json:"error,omitempty"`
}

type report struct {
	Passed bool    `json:"passed"`
	Checks []check `json:"checks"`
}

func main() {
	resultPath := flag.String("result", "", "evaluation result JSON file")
	baselinePath := flag.String("baseline", "evaluation/baseline.json", "baseline threshold JSON file")
	reportPath := flag.String("report", "", "optional report JSON output file")
	flag.Parse()
	if *resultPath == "" {
		fmt.Fprintln(os.Stderr, "-result is required")
		os.Exit(2)
	}

	result, err := readJSONMap(*resultPath)
	if err != nil {
		fatal(err)
	}
	configBytes, err := os.ReadFile(*baselinePath)
	if err != nil {
		fatal(err)
	}
	var config baseline
	if err := json.Unmarshal(configBytes, &config); err != nil {
		fatal(fmt.Errorf("parse baseline: %w", err))
	}

	report := evaluate(unwrapAPIData(result), config)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(encoded))
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, append(encoded, '\n'), 0o644); err != nil {
			fatal(err)
		}
	}
	if !report.Passed {
		os.Exit(1)
	}
}

func evaluate(result map[string]any, config baseline) report {
	report := report{Passed: true}
	if config.RequireSuccess {
		actual, ok := lookup(result, "task.status")
		passed := ok && isSuccessStatus(actual)
		report.Checks = append(report.Checks, check{
			Path: "task.status", Rule: "success", Passed: passed,
			Error: missingError(ok),
		})
		report.Passed = report.Passed && passed
	}
	if config.RequireComplete {
		finished, finishedOK := numberAt(result, "task.finished")
		total, totalOK := numberAt(result, "task.total")
		passed := finishedOK && totalOK && total > 0 && finished == total
		report.Checks = append(report.Checks, check{
			Path: "task.finished", Rule: "equals task.total", Expected: total,
			Actual: finished, Passed: passed, Error: missingError(finishedOK && totalOK),
		})
		report.Passed = report.Passed && passed
	}
	appendThresholdChecks := func(values map[string]float64, rule string) {
		paths := make([]string, 0, len(values))
		for path := range values {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			expected := values[path]
			actual, ok := numberAt(result, path)
			passed := ok && ((rule == "minimum" && actual >= expected) ||
				(rule == "maximum" && actual <= expected))
			report.Checks = append(report.Checks, check{
				Path: path, Rule: rule, Expected: expected, Actual: actual,
				Passed: passed, Error: missingError(ok),
			})
			report.Passed = report.Passed && passed
		}
	}
	appendThresholdChecks(config.Minimum, "minimum")
	appendThresholdChecks(config.Maximum, "maximum")
	return report
}

func unwrapAPIData(value map[string]any) map[string]any {
	if data, ok := value["data"].(map[string]any); ok {
		return data
	}
	return value
}

func lookup(root map[string]any, path string) (any, bool) {
	var current any = root
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func numberAt(root map[string]any, path string) (float64, bool) {
	value, ok := lookup(root, path)
	if !ok {
		return 0, false
	}
	number, ok := value.(float64)
	return number, ok
}

func isSuccessStatus(value any) bool {
	switch status := value.(type) {
	case float64:
		return status == 2
	case string:
		return strings.EqualFold(status, "success") || strings.EqualFold(status, "succeeded")
	default:
		return false
	}
}

func missingError(ok bool) string {
	if ok {
		return ""
	}
	return "value is missing or not numeric"
}

func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse result: %w", err)
	}
	if result == nil {
		return nil, errors.New("result must be a JSON object")
	}
	return result, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
