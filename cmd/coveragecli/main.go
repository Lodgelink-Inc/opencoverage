package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type globList []string

func (g *globList) String() string {
	if len(*g) == 0 {
		return ""
	}
	return strings.Join(*g, ",")
}

func (g *globList) Set(value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return fmt.Errorf("glob cannot be empty")
	}
	*g = append(*g, v)
	return nil
}

type packageCoverage struct {
	ImportPath      string  `json:"importPath"`
	CoveragePercent float64 `json:"coveragePercent"`
}

type ingestPayload struct {
	ProjectKey           string            `json:"projectKey"`
	ProjectName          string            `json:"projectName,omitempty"`
	ProjectGroup         *string           `json:"projectGroup,omitempty"`
	DefaultBranch        string            `json:"defaultBranch,omitempty"`
	Branch               string            `json:"branch"`
	CommitSHA            string            `json:"commitSha"`
	Author               string            `json:"author,omitempty"`
	TriggerType          string            `json:"triggerType"`
	RunTimestamp         string            `json:"runTimestamp"`
	TotalCoveragePercent float64           `json:"totalCoveragePercent"`
	ThresholdPercent     *float64          `json:"thresholdPercent,omitempty"`
	Packages             []packageCoverage `json:"packages"`
}

type integrationPayload struct {
	ProjectKey    string         `json:"projectKey"`
	ProjectName   string         `json:"projectName,omitempty"`
	ProjectGroup  *string        `json:"projectGroup,omitempty"`
	DefaultBranch string         `json:"defaultBranch,omitempty"`
	Branch        string         `json:"branch"`
	CommitSHA     string         `json:"commitSha"`
	Author        string         `json:"author,omitempty"`
	TriggerType   string         `json:"triggerType"`
	RunTimestamp  string         `json:"runTimestamp"`
	Environment   *string        `json:"environment,omitempty"`
	GinkgoReport  map[string]any `json:"ginkgoReport"`
}

type e2ePayload struct {
	ProjectKey    string         `json:"projectKey"`
	ProjectName   string         `json:"projectName,omitempty"`
	ProjectGroup  *string        `json:"projectGroup,omitempty"`
	DefaultBranch string         `json:"defaultBranch,omitempty"`
	Branch        string         `json:"branch"`
	CommitSHA     string         `json:"commitSha"`
	Author        string         `json:"author,omitempty"`
	TriggerType   string         `json:"triggerType"`
	RunTimestamp  string         `json:"runTimestamp"`
	Environment   *string        `json:"environment,omitempty"`
	TestReport    map[string]any `json:"testReport"`
}

type uploadResponse struct {
	Run struct {
		Status          string  `json:"status"`
		PassRatePercent float64 `json:"passRatePercent"`
	} `json:"run"`
	Comparison struct {
		DeltaPercent *float64 `json:"deltaPercent"`
	} `json:"comparison"`
}

type vitestMetric struct {
	Total   float64 `json:"total"`
	Covered float64 `json:"covered"`
	Skipped float64 `json:"skipped"`
	Pct     float64 `json:"pct"`
}

type vitestSummaryEntry struct {
	Lines      vitestMetric `json:"lines"`
	Statements vitestMetric `json:"statements"`
	Functions  vitestMetric `json:"functions"`
	Branches   vitestMetric `json:"branches"`
}

type metricAgg struct {
	Covered float64
	Total   float64
}

// JUnit XML structs — shared between Playwright and Appium JUnit reports.
// JUnitTestSuites represents the root <testsuites> element in JUnit XML.
type JUnitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	Name       string           `xml:"name,attr,omitempty"`
	Tests      int              `xml:"tests,attr,omitempty"`
	Failures   int              `xml:"failures,attr,omitempty"`
	Errors     int              `xml:"errors,attr,omitempty"`
	Time       float64          `xml:"time,attr,omitempty"`
	TestSuites []JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite represents a single <testsuite> element in JUnit XML.
type JUnitTestSuite struct {
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr,omitempty"`
	Failures   int             `xml:"failures,attr,omitempty"`
	Errors     int             `xml:"errors,attr,omitempty"`
	Skipped    int             `xml:"skipped,attr,omitempty"`
	Time       float64         `xml:"time,attr,omitempty"`
	Timestamp  string          `xml:"timestamp,attr,omitempty"`
	Hostname   string          `xml:"hostname,attr,omitempty"`
	Properties []JUnitProperty `xml:"properties>property,omitempty"`
	TestCases  []JUnitTestCase `xml:"testcase"`
	SystemOut  string          `xml:"system-out,omitempty"`
}

type JUnitTestCase struct {
	Classname string        `xml:"classname,attr,omitempty"`
	Name      string        `xml:"name,attr"`
	Time      float64       `xml:"time,attr,omitempty"`
	Status    string        `xml:"status,attr,omitempty"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
}

type JUnitFailure struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

type JUnitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

type JUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// JaCoCo XML structs — for parsing mobile (Android) coverage reports.
// JacocoReport represents the root <report> element in JaCoCo XML.
type JacocoReport struct {
	XMLName  xml.Name        `xml:"report"`
	Packages []JacocoPackage `xml:"package"`
	Counters []JacocoCounter `xml:"counter"`
}

// JacocoPackage represents a <package> element containing classes.
type JacocoPackage struct {
	Name     string          `xml:"name,attr"`
	Classes  []JacocoClass   `xml:"class"`
	Counters []JacocoCounter `xml:"counter"`
}

// JacocoClass represents a <class> element with counters.
type JacocoClass struct {
	Name     string          `xml:"name,attr"`
	Counters []JacocoCounter `xml:"counter"`
}

// JacocoCounter represents a <counter> element with coverage metrics.
type JacocoCounter struct {
	Type    string `xml:"type,attr"`
	Missed  int    `xml:"missed,attr"`
	Covered int    `xml:"covered,attr"`
}

// Sonar Generic Coverage XML structs (XCResult converted via Sonar tools).
// SonarCoverage represents the root <coverage> element.
type SonarCoverage struct {
	XMLName xml.Name    `xml:"coverage"`
	Files   []SonarFile `xml:"file"`
}

// SonarFile represents a <file> element with line coverage data.
type SonarFile struct {
	Path  string             `xml:"path,attr"`
	Lines []SonarLineToCover `xml:"lineToCover"`
}

// SonarLineToCover represents a <lineToCover> element.
type SonarLineToCover struct {
	Covered bool `xml:"covered,attr"`
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "integration-upload":
			runIntegrationUpload(os.Args[2:])
			return
		case "e2e-upload":
			runE2EUpload(os.Args[2:])
			return
		case "npm-upload":
			runNPMUpload(os.Args[2:])
			return
		case "mobile-coverage":
			runMobileCoverage(os.Args[2:])
			return
		}
	}

	runCoverageUpload(os.Args[1:])
}

func runNPMUpload(args []string) {
	fs := flag.NewFlagSet("npm-upload", flag.ExitOnError)
	summaryPath := fs.String("vitest-summary", "", "Path to Vitest coverage summary JSON")
	apiURL := fs.String("api-url", envOrDefault("API_URL", "http://localhost:8080/v1/coverage-runs"), "Coverage API URL")
	apiKey := fs.String("api-key", os.Getenv("API_KEY"), "API key value")
	apiKeyHeader := fs.String("api-key-header", "X-API-Key", "API key header name")
	projectKey := fs.String("project-key", envOrDefault("COVERAGE_PROJECT_KEY", "github.com/arxdsilva/opencoverage"), "Project key")
	projectName := fs.String("project-name", envOrDefault("COVERAGE_PROJECT_NAME", "coverage-api"), "Project display name")
	projectGroup := fs.String("project-group", "", "Project group (optional)")
	defaultBranch := fs.String("default-branch", envOrDefault("COVERAGE_DEFAULT_BRANCH", "main"), "Default branch")
	branch := fs.String("branch", envOrDefault("COVERAGE_BRANCH", "main"), "Current branch")
	commitSHA := fs.String("commit-sha", envOrDefault("COVERAGE_COMMIT_SHA", "local"), "Commit SHA")
	author := fs.String("author", envOrDefault("COVERAGE_AUTHOR", "local"), "Author")
	triggerType := fs.String("trigger-type", "manual", "Trigger type: push|pr|manual")
	runTimestamp := fs.String("run-timestamp", time.Now().UTC().Format(time.RFC3339), "Run timestamp (RFC3339)")
	threshold := fs.Float64("threshold", 0, "Custom threshold percentage (0 to disable custom threshold)")
	metric := fs.String("metric", "lines", "Metric used for totals: lines|statements|functions|branches")
	groupBy := fs.String("group-by", "dir", "Grouping strategy: dir|file")
	pathStripPrefix := fs.String("path-strip-prefix", "", "Path prefix to remove from file keys")
	out := fs.String("out", "", "Optional path to write generated payload")
	dryRun := fs.Bool("dry-run", false, "Generate payload without upload")
	var includeGlobs globList
	var excludeGlobs globList
	fs.Var(&includeGlobs, "include-glob", "Include files matching this glob (repeatable)")
	fs.Var(&excludeGlobs, "exclude-glob", "Exclude files matching this glob (repeatable)")

	if err := fs.Parse(args); err != nil {
		exitErr("parse flags", err)
	}

	if strings.TrimSpace(*summaryPath) == "" {
		exitErr("validate input", fmt.Errorf("ERR_INPUT_SCHEMA: -vitest-summary is required"))
	}
	if _, err := time.Parse(time.RFC3339, *runTimestamp); err != nil {
		exitErr("validate input", fmt.Errorf("ERR_INPUT_SCHEMA: run timestamp must be RFC3339: %w", err))
	}

	total, packages, consideredFiles, err := parseVitestSummary(
		*summaryPath,
		*metric,
		*groupBy,
		*pathStripPrefix,
		includeGlobs,
		excludeGlobs,
	)
	if err != nil {
		exitErr("parse vitest summary", err)
	}

	var group *string
	if *projectGroup != "" {
		group = projectGroup
	}

	var thresh *float64
	if *threshold > 0 {
		thresh = threshold
	}

	slog.Info("summary", "metric", *metric, "totalCoveragePercent", total, "consideredFiles", consideredFiles, "generatedPackages", len(packages))

	payload := ingestPayload{
		ProjectKey:           *projectKey,
		ProjectName:          *projectName,
		ProjectGroup:         group,
		DefaultBranch:        *defaultBranch,
		Branch:               *branch,
		CommitSHA:            *commitSHA,
		Author:               *author,
		TriggerType:          *triggerType,
		RunTimestamp:         *runTimestamp,
		TotalCoveragePercent: total,
		ThresholdPercent:     thresh,
		Packages:             packages,
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		exitErr("marshal payload", err)
	}

	payloadOut := strings.TrimSpace(*out)
	if *dryRun && payloadOut == "" {
		payloadOut = "npm-coverage-upload.json"
	}
	if payloadOut != "" {
		if err := os.WriteFile(payloadOut, body, 0o644); err != nil {
			exitErr("write payload", err)
		}
		slog.Info("payload written", "path", payloadOut)
	}

	if *dryRun {
		fmt.Println("dry-run enabled: skipping upload")
		return
	}

	if strings.TrimSpace(*apiKey) == "" {
		exitErr("validate input", fmt.Errorf("ERR_INPUT_SCHEMA: -api-key is required (or API_KEY env var)"))
	}

	status, respBody, err := uploadPayload(*apiURL, *apiKeyHeader, *apiKey, body)
	if err != nil {
		exitErr("upload", fmt.Errorf("ERR_UPLOAD_FAILED: %w", err))
	}

	slog.Info("upload status", "status", status)
	slog.Info("upload response", "response", strings.TrimSpace(string(respBody)))

	if status >= http.StatusBadRequest {
		exitErr("upload", fmt.Errorf("ERR_UPLOAD_FAILED: server returned status %d", status))
	}
}

func runMobileCoverage(args []string) {
	fs := flag.NewFlagSet("mobile-coverage", flag.ExitOnError)
	reportPath := fs.String("report", "", "Path to coverage report (JaCoCo XML or Sonar Generic XML)")
	formatFlag := fs.String("format", "jacoco", "Report format: jacoco|sonar")
	apiURL := fs.String("api-url", envOrDefault("API_URL", "http://localhost:8080/v1/coverage-runs"), "Coverage API URL")
	apiKey := fs.String("api-key", os.Getenv("API_KEY"), "API key value")
	apiKeyHeader := fs.String("api-key-header", "X-API-Key", "API key header name")
	projectKey := fs.String("project-key", envOrDefault("COVERAGE_PROJECT_KEY", "github.com/arxdsilva/opencoverage"), "Project key")
	projectName := fs.String("project-name", envOrDefault("COVERAGE_PROJECT_NAME", "coverage-api"), "Project display name")
	projectGroup := fs.String("project-group", "", "Project group (optional)")
	defaultBranch := fs.String("default-branch", envOrDefault("COVERAGE_DEFAULT_BRANCH", "main"), "Default branch")
	branch := fs.String("branch", envOrDefault("COVERAGE_BRANCH", "main"), "Current branch")
	commitSHA := fs.String("commit-sha", envOrDefault("COVERAGE_COMMIT_SHA", "local"), "Commit SHA")
	author := fs.String("author", envOrDefault("COVERAGE_AUTHOR", "local"), "Author")
	triggerType := fs.String("trigger-type", "manual", "Trigger type: push|pr|manual")
	runTimestamp := fs.String("run-timestamp", time.Now().UTC().Format(time.RFC3339), "Run timestamp (RFC3339)")
	threshold := fs.Float64("threshold", 0, "Custom threshold percentage (0 to disable custom threshold)")
	metric := fs.String("metric", "line", "Metric used for totals: line|instruction|branch|method|complexity")
	groupBy := fs.String("group-by", "dir", "Grouping strategy: file|dir")
	out := fs.String("out", "", "Optional path to write generated payload")
	dryRun := fs.Bool("dry-run", false, "Generate payload without upload")
	var includeGlobs globList
	var excludeGlobs globList
	fs.Var(&includeGlobs, "include-glob", "Include packages matching this glob (repeatable)")
	fs.Var(&excludeGlobs, "exclude-glob", "Exclude packages matching this glob (repeatable)")

	if err := fs.Parse(args); err != nil {
		exitErr("parse flags", err)
	}

	if strings.TrimSpace(*reportPath) == "" {
		exitErr("validate input", fmt.Errorf("ERR_INPUT_SCHEMA: -report is required"))
	}
	if _, err := time.Parse(time.RFC3339, *runTimestamp); err != nil {
		exitErr("validate input", fmt.Errorf("ERR_INPUT_SCHEMA: run timestamp must be RFC3339: %w", err))
	}

	format := strings.ToLower(strings.TrimSpace(*formatFlag))

	var total float64
	var packages []packageCoverage
	var consideredCount int
	var parseErr error

	switch format {
	case "jacoco":
		total, packages, consideredCount, parseErr = parseJacocoReport(
			*reportPath,
			*metric,
			*groupBy,
			includeGlobs,
			excludeGlobs,
		)
	case "sonar":
		total, packages, consideredCount, parseErr = parseSonarCoverage(
			*reportPath,
			*groupBy,
			includeGlobs,
			excludeGlobs,
		)
	default:
		exitErr("validate input", fmt.Errorf("ERR_INPUT_SCHEMA: unsupported format %q; valid: jacoco, sonar", format))
	}
	if parseErr != nil {
		exitErr("parse coverage report", parseErr)
	}

	// Metric flag is only used for JaCoCo; Sonar only supports line coverage.
	metricLabel := *metric
	if format == "sonar" {
		metricLabel = "line"
	}

	var group *string
	if *projectGroup != "" {
		group = projectGroup
	}

	var thresh *float64
	if *threshold > 0 {
		thresh = threshold
	}

	slog.Info("summary", "metric", metricLabel, "totalCoveragePercent", total, "consideredPackages", consideredCount, "generatedPackages", len(packages))

	payload := ingestPayload{
		ProjectKey:           *projectKey,
		ProjectName:          *projectName,
		ProjectGroup:         group,
		DefaultBranch:        *defaultBranch,
		Branch:               *branch,
		CommitSHA:            *commitSHA,
		Author:               *author,
		TriggerType:          *triggerType,
		RunTimestamp:         *runTimestamp,
		TotalCoveragePercent: total,
		ThresholdPercent:     thresh,
		Packages:             packages,
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		exitErr("marshal payload", err)
	}

	payloadOut := strings.TrimSpace(*out)
	if *dryRun && payloadOut == "" {
		payloadOut = "mobile-coverage-upload.json"
	}
	if payloadOut != "" {
		if err := os.WriteFile(payloadOut, body, 0o644); err != nil {
			exitErr("write payload", err)
		}
		slog.Info("payload written", "path", payloadOut)
	}

	if *dryRun {
		fmt.Println("dry-run enabled: skipping upload")
		return
	}

	if strings.TrimSpace(*apiKey) == "" {
		exitErr("validate input", fmt.Errorf("ERR_INPUT_SCHEMA: -api-key is required (or API_KEY env var)"))
	}

	status, respBody, err := uploadPayload(*apiURL, *apiKeyHeader, *apiKey, body)
	if err != nil {
		exitErr("upload", fmt.Errorf("ERR_UPLOAD_FAILED: %w", err))
	}

	slog.Info("upload status", "status", status)
	slog.Info("upload response", "response", strings.TrimSpace(string(respBody)))

	if status >= http.StatusBadRequest {
		exitErr("upload", fmt.Errorf("ERR_UPLOAD_FAILED: server returned status %d", status))
	}
}

func runCoverageUpload(args []string) {
	fs := flag.NewFlagSet("coveragecli", flag.ExitOnError)
	coverprofile := fs.String("coverprofile", "coverage.out", "Path to go coverage profile")
	out := fs.String("out", "coverage-upload.json", "Path to output JSON payload file")
	projectKey := fs.String("project-key", "github.com/arxdsilva/opencoverage", "Project key")
	projectName := fs.String("project-name", "coverage-api", "Project display name")
	projectGroup := fs.String("project-group", "", "Project group (optional)")
	defaultBranch := fs.String("default-branch", "main", "Default branch")
	branch := fs.String("branch", envOrDefault("COVERAGE_BRANCH", "main"), "Current branch")
	commitSHA := fs.String("commit-sha", envOrDefault("COVERAGE_COMMIT_SHA", "local"), "Commit SHA")
	author := fs.String("author", envOrDefault("COVERAGE_AUTHOR", "local"), "Author")
	triggerType := fs.String("trigger-type", "manual", "Trigger type: push|pr|manual")
	threshold := fs.Float64("threshold", 0, "Custom threshold percentage (0 to disable custom threshold)")
	upload := fs.Bool("upload", false, "Upload payload to API")
	apiURL := fs.String("api-url", envOrDefault("API_URL", "http://localhost:8080/v1/coverage-runs"), "Coverage API URL")
	apiKey := fs.String("api-key", os.Getenv("API_KEY"), "API key value")
	apiKeyHeader := fs.String("api-key-header", "X-API-Key", "API key header name")
	if err := fs.Parse(args); err != nil {
		exitErr("parse flags", err)
	}

	total, packages, err := parseCoverage(*coverprofile)
	if err != nil {
		exitErr("parse coverage", err)
	}
	if len(packages) == 0 {
		exitErr("parse coverage", fmt.Errorf("no package coverage entries found"))
	}

	var group *string
	if *projectGroup != "" {
		group = projectGroup
	}

	var thresh *float64
	if *threshold > 0 {
		thresh = threshold
	}

	payload := ingestPayload{
		ProjectKey:           *projectKey,
		ProjectName:          *projectName,
		ProjectGroup:         group,
		DefaultBranch:        *defaultBranch,
		Branch:               *branch,
		CommitSHA:            *commitSHA,
		Author:               *author,
		TriggerType:          *triggerType,
		RunTimestamp:         time.Now().UTC().Format(time.RFC3339),
		TotalCoveragePercent: total,
		ThresholdPercent:     thresh,
		Packages:             packages,
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		exitErr("marshal payload", err)
	}

	if err := os.WriteFile(*out, body, 0o644); err != nil {
		exitErr("write payload file", err)
	}
	slog.Info("payload written", "path", *out)

	if !*upload {
		return
	}
	if strings.TrimSpace(*apiKey) == "" {
		exitErr("upload", fmt.Errorf("api key is required when -upload is set (use -api-key or API_KEY env var)"))
	}

	status, respBody, err := uploadPayload(*apiURL, *apiKeyHeader, *apiKey, body)
	if err != nil {
		exitErr("upload", err)
	}

	slog.Info("upload status", "status", status)
	slog.Info("upload response", "response", strings.TrimSpace(string(respBody)))
}

func runIntegrationUpload(args []string) {
	fs := flag.NewFlagSet("integration-upload", flag.ExitOnError)
	reportPath := fs.String("ginkgo-report", "", "Path to Ginkgo JSON report")
	apiURL := fs.String("api-url", envOrDefault("API_URL", "http://localhost:8080/v1/integration-test-runs"), "Integration test API URL")
	apiKey := fs.String("api-key", os.Getenv("API_KEY"), "API key value")
	apiKeyHeader := fs.String("api-key-header", "X-API-Key", "API key header name")
	projectKey := fs.String("project-key", envOrDefault("COVERAGE_PROJECT_KEY", "github.com/arxdsilva/opencoverage"), "Project key")
	projectName := fs.String("project-name", envOrDefault("COVERAGE_PROJECT_NAME", "coverage-api"), "Project display name")
	projectGroup := fs.String("project-group", "", "Project group (optional)")
	defaultBranch := fs.String("default-branch", envOrDefault("COVERAGE_DEFAULT_BRANCH", "main"), "Default branch")
	branch := fs.String("branch", envOrDefault("COVERAGE_BRANCH", "main"), "Current branch")
	commitSHA := fs.String("commit-sha", envOrDefault("COVERAGE_COMMIT_SHA", "local"), "Commit SHA")
	author := fs.String("author", envOrDefault("COVERAGE_AUTHOR", "local"), "Author")
	triggerType := fs.String("trigger-type", "manual", "Trigger type: push|pr|manual")
	environment := fs.String("environment", "", "Environment: test|stage|prod (optional)")
	runTimestamp := fs.String("run-timestamp", time.Now().UTC().Format(time.RFC3339), "Run timestamp (RFC3339)")
	if err := fs.Parse(args); err != nil {
		exitErr("parse flags", err)
	}

	if strings.TrimSpace(*reportPath) == "" {
		exitErr("validate input", fmt.Errorf("-ginkgo-report is required"))
	}
	if strings.TrimSpace(*apiKey) == "" {
		exitErr("validate input", fmt.Errorf("-api-key is required (or API_KEY env var)"))
	}
	if _, err := time.Parse(time.RFC3339, *runTimestamp); err != nil {
		exitErr("validate input", fmt.Errorf("run timestamp must be RFC3339: %w", err))
	}

	rawReport, err := os.ReadFile(*reportPath)
	if err != nil {
		exitErr("read ginkgo report", err)
	}

	var report map[string]any
	if err := json.Unmarshal(rawReport, &report); err != nil {
		exitErr("parse ginkgo report json", err)
	}

	var group *string
	if *projectGroup != "" {
		group = projectGroup
	}

	var env *string
	if *environment != "" {
		if *environment != "test" && *environment != "stage" && *environment != "prod" {
			exitErr("validate input", fmt.Errorf("-environment must be one of: test, stage, prod"))
		}
		env = environment
	}

	payload := integrationPayload{
		ProjectKey:    *projectKey,
		ProjectName:   *projectName,
		ProjectGroup:  group,
		DefaultBranch: *defaultBranch,
		Branch:        *branch,
		CommitSHA:     *commitSHA,
		Author:        *author,
		TriggerType:   *triggerType,
		RunTimestamp:  *runTimestamp,
		Environment:   env,
		GinkgoReport:  normalizeReport(report),
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		exitErr("marshal payload", err)
	}

	status, respBody, err := uploadPayload(*apiURL, *apiKeyHeader, *apiKey, body)
	if err != nil {
		exitErr("upload integration report", err)
	}

	slog.Info("upload status", "status", status)
	slog.Info("upload response", "response", strings.TrimSpace(string(respBody)))

	var parsed uploadResponse
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		delta := "-"
		if parsed.Comparison.DeltaPercent != nil {
			delta = fmt.Sprintf("%.2f", *parsed.Comparison.DeltaPercent)
		}
		slog.Info("summary", "status", parsed.Run.Status, "passRatePercent", parsed.Run.PassRatePercent, "deltaPercent", delta)
	}

	if status >= http.StatusBadRequest {
		exitErr("upload integration report", fmt.Errorf("server returned status %d", status))
	}
}

func runE2EUpload(args []string) {
	fs := flag.NewFlagSet("e2e-upload", flag.ExitOnError)
	reportPath := fs.String("e2e-report", "", "Path to e2e JSON report")
	reportType := fs.String("report-type", "playwright", "E2E report type")
	apiURL := fs.String("api-url", envOrDefault("API_URL", "http://localhost:8080/v1/e2e-test-runs"), "E2E test API URL")
	apiKey := fs.String("api-key", os.Getenv("API_KEY"), "API key value")
	apiKeyHeader := fs.String("api-key-header", "X-API-Key", "API key header name")
	projectKey := fs.String("project-key", envOrDefault("COVERAGE_PROJECT_KEY", "github.com/arxdsilva/opencoverage"), "Project key")
	projectName := fs.String("project-name", envOrDefault("COVERAGE_PROJECT_NAME", "coverage-api"), "Project display name")
	projectGroup := fs.String("project-group", "", "Project group (optional)")
	defaultBranch := fs.String("default-branch", envOrDefault("COVERAGE_DEFAULT_BRANCH", "main"), "Default branch")
	branch := fs.String("branch", envOrDefault("COVERAGE_BRANCH", "main"), "Current branch")
	commitSHA := fs.String("commit-sha", envOrDefault("COVERAGE_COMMIT_SHA", "local"), "Commit SHA")
	author := fs.String("author", envOrDefault("COVERAGE_AUTHOR", "local"), "Author")
	triggerType := fs.String("trigger-type", "manual", "Trigger type: push|pr|manual")
	environment := fs.String("environment", "", "Environment: test|stage|prod (optional)")
	runTimestamp := fs.String("run-timestamp", time.Now().UTC().Format(time.RFC3339), "Run timestamp (RFC3339)")

	if err := fs.Parse(args); err != nil {
		exitErr("parse flags", err)
	}

	if strings.TrimSpace(*reportPath) == "" {
		exitErr("validate input", fmt.Errorf("-e2e-report is required"))
	}
	if strings.TrimSpace(*apiKey) == "" {
		exitErr("validate input", fmt.Errorf("-api-key is required (or API_KEY env var)"))
	}
	if _, err := time.Parse(time.RFC3339, *runTimestamp); err != nil {
		exitErr("validate input", fmt.Errorf("run timestamp must be RFC3339: %w", err))
	}

	rawReport, err := os.ReadFile(*reportPath)
	if err != nil {
		exitErr("read e2e report", err)
	}

	// Detect file format from extension
	ext := strings.ToLower(filepath.Ext(*reportPath))
	var isXML bool
	switch ext {
	case ".xml":
		isXML = true
	case ".json":
		isXML = false
	default:
		exitErr("validate input", fmt.Errorf("unsupported file extension %q: expected .json or .xml", ext))
	}

	var group *string
	if *projectGroup != "" {
		group = projectGroup
	}

	var env *string
	if *environment != "" {
		if *environment != "test" && *environment != "stage" && *environment != "prod" {
			exitErr("validate input", fmt.Errorf("-environment must be one of: test, stage, prod"))
		}
		env = environment
	}

	// Normalize report structure based on report type and file format
	var normalizedReport map[string]any
	if isXML {
		normalizedReport, err = normalizeE2EXMLReport(rawReport, reportType)
		if err != nil {
			exitErr("parse e2e report xml", err)
		}
	} else {
		normalizedReport, err = normalizeE2EJsonReport(rawReport, reportType)
		if err != nil {
			exitErr("parse e2e report json", err)
		}
	}

	payload := e2ePayload{
		ProjectKey:    *projectKey,
		ProjectName:   *projectName,
		ProjectGroup:  group,
		DefaultBranch: *defaultBranch,
		Branch:        *branch,
		CommitSHA:     *commitSHA,
		Author:        *author,
		TriggerType:   *triggerType,
		RunTimestamp:  *runTimestamp,
		Environment:   env,
		TestReport:    normalizedReport,
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		exitErr("marshal payload", err)
	}

	status, respBody, err := uploadPayload(*apiURL, *apiKeyHeader, *apiKey, body)
	if err != nil {
		exitErr("upload report", err)
	}

	var parsed uploadResponse
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		delta := "-"
		if parsed.Comparison.DeltaPercent != nil {
			delta = fmt.Sprintf("%.2f", *parsed.Comparison.DeltaPercent)
		}
		slog.Info("summary", "status", parsed.Run.Status, "passRatePercent", parsed.Run.PassRatePercent, "deltaPercent", delta)
	}

	if status >= http.StatusBadRequest {
		exitErr("upload report", fmt.Errorf("server returned status %d", status))
	}
}

// unmarshal the rawReport XML into JUnitTestSuites and normalize it based on the reportType (playwright or appium).
func normalizeE2EXMLReport(rawReport []byte, reportType *string) (normalizedReport map[string]any, err error) {
	var junitData JUnitTestSuites
	if err := xml.Unmarshal(rawReport, &junitData); err != nil {
		return nil, fmt.Errorf("parse e2e report xml: %w", err)
	}
	switch *reportType {
	case "playwright":
		normalizedReport, err = normalizePlaywrightJUnit(junitData)
		if err != nil {
			return nil, fmt.Errorf("normalize playwright junit: %w", err)
		}
	case "appium":
		normalizedReport, err = normalizeAppiumJUnit(junitData)
		if err != nil {
			return nil, fmt.Errorf("normalize appium junit: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported report type: %s", *reportType)
	}
	return normalizedReport, nil
}

// unmarshal the rawReport JSON into a map[string]any and normalize it based on the reportType (playwright or appium).
func normalizeE2EJsonReport(rawReport []byte, reportType *string) (normalizedReport map[string]any, err error) {
	var report map[string]any
	if err := json.Unmarshal(rawReport, &report); err != nil {
		return nil, fmt.Errorf("parse e2e report json: %w", err)
	}
	switch *reportType {
	case "playwright":
		normalizedReport = normalizePlaywrightReport(report)
	case "appium":
		return nil, fmt.Errorf("appium JSON report format is not yet supported; use JUnit XML (.xml)")
	default:
		return nil, fmt.Errorf("unsupported report type: %s", *reportType)
	}
	return normalizedReport, nil
}

func normalizeReport(raw map[string]any) map[string]any {
	result := make(map[string]any)
	result["suiteDescription"] = firstString(raw, "suiteDescription", "SuiteDescription")
	result["suitePath"] = firstString(raw, "suitePath", "SuitePath")
	result["ginkgoVersion"] = firstString(raw, "ginkgoVersion", "GinkgoVersion")

	specReports := firstSlice(raw, "specReports", "SpecReports")
	normalizedSpecs := make([]map[string]any, 0, len(specReports))
	for _, item := range specReports {
		specMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		normalized := map[string]any{
			"leafNodeText":            firstString(specMap, "leafNodeText", "LeafNodeText"),
			"containerHierarchyTexts": firstSlice(specMap, "containerHierarchyTexts", "ContainerHierarchyTexts"),
			"state":                   firstString(specMap, "state", "State"),
			"runTime":                 firstFloat(specMap, "runTime", "RunTime"),
		}

		failureVal := firstMap(specMap, "failure", "Failure")
		if len(failureVal) > 0 {
			failure := map[string]any{
				"message": firstString(failureVal, "message", "Message"),
			}
			locationVal := firstMap(failureVal, "location", "Location")
			if len(locationVal) > 0 {
				failure["location"] = map[string]any{
					"fileName":   firstString(locationVal, "fileName", "FileName"),
					"lineNumber": int(firstFloat(locationVal, "lineNumber", "LineNumber")),
				}
			}
			normalized["failure"] = failure
		}

		normalizedSpecs = append(normalizedSpecs, normalized)
	}

	result["specReports"] = normalizedSpecs
	return result
}

func normalizePlaywrightReport(raw map[string]any) map[string]any {
	var suiteDescription string
	var suitePath string
	var framework_version string

	result := make(map[string]any)
	testFramework := "playwright"

	config := firstMap(raw, "config")
	suites := firstSlice(raw, "suites")
	if config != nil {
		suitePath = firstString(config, "rootDir")
		framework_version = firstString(config, "version")
	}
	if len(suites) > 0 {
		if first, ok := suites[0].(map[string]any); ok {
			suiteDescription = firstString(first, "title")
		}
	}
	result["suiteDescription"] = suiteDescription
	result["suitePath"] = suitePath
	result["reportType"] = &testFramework
	result["testFramework"] = &testFramework
	result["frameworkVersion"] = framework_version
	result["platformType"] = "web"

	// collectSpecs recursively walks Playwright's nested suite tree,
	// accumulating containerHierarchyTexts as it descends, and normalises each leaf spec
	// Suites can be nested N level deep and leaf specs can be at any level, so we need to recurse fully to find all specs and get their full hierarchy.
	var collectSpecs func(suites []any, hierarchy []string) []map[string]any
	collectSpecs = func(suites []any, hierarchy []string) []map[string]any {
		var out []map[string]any
		// iterates over all the suites branches at the current level
		for _, item := range suites {
			suiteMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			title := firstString(suiteMap, "title")
			currentHierarchy := hierarchy
			// appends hierarchy with current suite title if it exists
			// coppies all elements from hierarchy into new slice to avoid mutating the original slice in recursive calls
			if title != "" {
				currentHierarchy = append(append([]string{}, hierarchy...), title)
			}

			// Recurse into nested suites leaves first.
			// as the suites can be nested N level deep
			// uses recursive calls to collect all leaf specs
			if nested := firstSlice(suiteMap, "suites"); len(nested) > 0 {
				out = append(out, collectSpecs(nested, currentHierarchy)...)
			}

			// Normalise leaf specs within this suite.
			for _, specItem := range firstSlice(suiteMap, "specs") {
				specMap, ok := specItem.(map[string]any)
				if !ok {
					slog.Warn("skipping spec with unexpected structure", "specItem", specItem)
					continue
				}

				// Use the last test result (accounts for retries).
				tests := firstSlice(specMap, "tests")
				file := firstString(suiteMap, "file")
				spec_type := "happyPath"
				state := "skipped"
				runTime := 0.0
				var failureBlock map[string]any

				if len(tests) > 0 {
					if testMap, ok := tests[0].(map[string]any); ok {
						switch firstString(testMap, "status") {
						case "expected":
							state = "passed"
						case "unexpected":
							state = "failed"
						case "flaky":
							state = "flaky"
						default:
							state = "skipped"
						}

						results := firstSlice(testMap, "results")
						if len(results) > 0 {
							// Use last result (final retry).
							if lastResult, ok := results[len(results)-1].(map[string]any); ok {
								// Playwright reports duration in ms; convert to seconds.
								runTime = firstFloat(lastResult, "duration") / 1000.0

								if errVal := firstMap(lastResult, "error"); len(errVal) > 0 {
									failure := map[string]any{
										"message": stripANSI(firstString(errVal, "message")),
									}
									if locVal := firstMap(errVal, "location"); len(locVal) > 0 {
										failure["location"] = map[string]any{
											"fileName":   firstString(locVal, "file"),
											"lineNumber": int(firstFloat(locVal, "line")),
										}
									}
									failureBlock = failure
								}
							}
						}

						// spec_type can be either setup, happyPath or negativePath
						// checks if the file name contains "setup", "happyPath" or "negativePath" to determine the spec_type
						// fall back to checking the projectId
						projectID := firstString(testMap, "projectId")

						switch {
						case strings.Contains(file, "setup"):
							spec_type = "setup"
						case strings.Contains(file, "happyPath"):
							spec_type = "happyPath"
						case strings.Contains(file, "negativePath"):
							spec_type = "negativePath"
						case projectID == "happypath" || projectID == "negativePath" || projectID == "setup":
							spec_type = projectID
						default:
							spec_type = "happyPath"
						}
					}
				}

				// copies currentHierarchy into new slice to avoid mutating the original slice in recursive calls
				hierarchyCopy := make([]any, len(currentHierarchy))
				for i, h := range currentHierarchy {
					hierarchyCopy[i] = h
				}

				normalized := map[string]any{
					"leafNodeText":            firstString(specMap, "title"),
					"containerHierarchyTexts": hierarchyCopy,
					"state":                   state,
					"runTime":                 runTime,
					"suite_type":              firstString(suiteMap, "type"),
					"specType":                spec_type,
				}
				if failureBlock != nil {
					normalized["failure"] = failureBlock
				}
				out = append(out, normalized)
			}
		}
		return out
	}
	result["specReports"] = collectSpecs(suites, nil)
	return result
}

// normalizePlaywrightJUnit converts a Playwright JUnit XML report into the normalized map[string]any structure.
// Playwright JUnit uses classname format: "file › Suite Title › Nested Suite"
func normalizePlaywrightJUnit(data JUnitTestSuites) (map[string]any, error) {
	return nil, fmt.Errorf("playwright JUnit XML normalization is not yet implemented")
}

// normalizeAppiumJUnit converts an Appium JUnit XML report into the normalized map[string]any structure.
// Appium JUnit uses classname format: "com.package.ClassName" (dot-separated)
func normalizeAppiumJUnit(data JUnitTestSuites) (map[string]any, error) {
	if len(data.TestSuites) == 0 {
		return nil, fmt.Errorf("ERR_INPUT_SCHEMA: appium JUnit report contains no <testsuite> elements")
	}
	result := make(map[string]any)
	testFramework := "appium"
	result["reportType"] = &testFramework
	result["testFramework"] = &testFramework

	// Use top-level testsuites name as suiteDescription
	if data.Name != "" {
		result["suiteDescription"] = data.Name
	}

	if len(data.TestSuites[0].TestCases) == 0 {
		return nil, fmt.Errorf("ERR_INPUT_SCHEMA: appium JUnit report contains no <testcase> elements")
	}
	result["suitePath"] = data.TestSuites[0].TestCases[0].Classname
	result["frameworkVersion"] = ""

	// Extract platform metadata from first testsuite's properties
	// Set default platform type for Appium
	platformType := "android"
	if len(data.TestSuites) > 0 {
		for _, prop := range data.TestSuites[0].Properties {
			switch prop.Name {
			case "platformName":
				platformType = strings.ToLower(prop.Value)
			case "automationName":
				result["frameworkVersion"] = prop.Value
			}
		}
	}
	result["platformType"] = platformType

	var specReports []map[string]any
	for _, suite := range data.TestSuites {
		for _, tc := range suite.TestCases {
			// Appium classname format: "com.package.tests.Login.LoginPass" (split on ".")
			var hierarchy []any
			if tc.Classname != "" {
				parts := strings.Split(tc.Classname, ".")
				for _, p := range parts {
					if p != "" {
						hierarchy = append(hierarchy, p)
					}
				}
			}

			// Determine state from failure/skipped elements or status attribute
			state := "passed"
			if tc.Failure != nil {
				state = "failed"
			} else if tc.Skipped != nil {
				state = "skipped"
			} else if tc.Status != "" {
				// Some Appium/TestNG reporters include a status attribute
				switch strings.ToLower(tc.Status) {
				case "passed":
					state = "passed"
				case "failed":
					state = "failed"
				case "skipped":
					state = "skipped"
				}
			}

			// Determine specType from classname keywords
			specType := "happyPath"
			classLower := strings.ToLower(tc.Classname)
			switch {
			case strings.Contains(classLower, "setup"):
				specType = "setup"
			case strings.Contains(classLower, "happypath"):
				specType = "happyPath"
			case strings.Contains(classLower, "negativepath"):
				specType = "negativePath"
			default:
				specType = "happyPath"
			}

			spec := map[string]any{
				"leafNodeText":            tc.Name,
				"containerHierarchyTexts": hierarchy,
				"state":                   state,
				"runTime":                 tc.Time,
				"suite_type":              suite.Name,
				"specType":                specType,
			}

			if tc.Failure != nil {
				failure := map[string]any{
					"message": tc.Failure.Message,
				}
				if tc.Failure.Body != "" {
					failure["stackTrace"] = strings.TrimSpace(tc.Failure.Body)
				}
				spec["failure"] = failure
			}
			specReports = append(specReports, spec)
		}
	}
	result["specReports"] = specReports
	return result, nil
}

// stripANSI removes ANSI escape codes from a string.
// This is useful to clean up error messages from Playwright which may include ANSI codes for coloring.
func stripANSI(s string) string {
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiRegex.ReplaceAllString(s, "")
}

func firstString(src map[string]any, keys ...string) string {
	for _, key := range keys {
		if raw, ok := src[key]; ok {
			if value, ok := raw.(string); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func firstFloat(src map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if raw, ok := src[key]; ok {
			switch v := raw.(type) {
			case float64:
				return v
			case int:
				return float64(v)
			case json.Number:
				f, err := v.Float64()
				if err == nil {
					return f
				}
			}
		}
	}
	return 0
}

func firstSlice(src map[string]any, keys ...string) []any {
	for _, key := range keys {
		if raw, ok := src[key]; ok {
			if value, ok := raw.([]any); ok {
				return value
			}
		}
	}
	return nil
}

func firstMap(src map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if raw, ok := src[key]; ok {
			if value, ok := raw.(map[string]any); ok {
				return value
			}
		}
	}
	return nil
}

// parseJacocoReport parses a JaCoCo XML coverage report and returns total coverage,
// per-package coverage, and the count of considered packages.
func parseJacocoReport(reportPath, metric, groupBy string, includeGlobs, excludeGlobs []string) (float64, []packageCoverage, int, error) {
	validMetrics := map[string]bool{
		"line": true, "instruction": true, "branch": true,
		"method": true, "complexity": true,
	}
	if !validMetrics[metric] {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: unsupported metric %q; valid: line, instruction, branch, method, complexity", metric)
	}
	if groupBy != "file" && groupBy != "dir" {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: unsupported group-by %q; valid: file, dir", groupBy)
	}

	raw, err := os.ReadFile(reportPath)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_READ: %w", err)
	}

	var report JacocoReport
	if err := xml.Unmarshal(raw, &report); err != nil {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_PARSE: %w", err)
	}

	// Extract report-level total for the selected metric.
	totalMissed, totalCovered, found := findJacocoCounter(report.Counters, metric)
	if !found {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: metric %q not found in report-level counters", metric)
	}
	totalSum := totalMissed + totalCovered
	if totalSum <= 0 {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: total coverage is zero (no executable code found)")
	}
	totalPercent := round2((float64(totalCovered) / float64(totalSum)) * 100)

	// Build per-package or per-class coverage.
	byGroup := make(map[string]metricAgg)
	consideredCount := 0

	for _, pkg := range report.Packages {
		// Convert JaCoCo package name (com/example/foo) to dot notation (com.example.foo).
		pkgName := strings.ReplaceAll(pkg.Name, "/", ".")

		// Apply glob filters at the package level.
		if len(includeGlobs) > 0 && !matchesAnyGlob(pkgName, includeGlobs) {
			continue
		}
		if matchesAnyGlob(pkgName, excludeGlobs) {
			continue
		}

		if groupBy == "dir" {
			missed, covered, ok := findJacocoCounter(pkg.Counters, metric)
			if !ok || (missed+covered) <= 0 {
				continue
			}
			agg := byGroup[pkgName]
			agg.Covered += float64(covered)
			agg.Total += float64(missed + covered)
			byGroup[pkgName] = agg
			consideredCount++
		}
	}

	if consideredCount == 0 || len(byGroup) == 0 {
		return 0, nil, 0, fmt.Errorf("ERR_EMPTY_DATASET: no packages remained after filtering")
	}

	pkgs := make([]packageCoverage, 0, len(byGroup))
	for groupKey, agg := range byGroup {
		if agg.Total <= 0 {
			continue
		}
		pct := round2((agg.Covered / agg.Total) * 100)
		pkgs = append(pkgs, packageCoverage{
			ImportPath:      groupKey,
			CoveragePercent: pct,
		})
	}

	if len(pkgs) == 0 {
		return 0, nil, 0, fmt.Errorf("ERR_EMPTY_DATASET: generated packages list is empty")
	}

	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return totalPercent, pkgs, consideredCount, nil
}

// findJacocoCounter searches for a counter of the given type and returns missed, covered, and found.
func findJacocoCounter(counters []JacocoCounter, metricType string) (missed, covered int, found bool) {
	// Map CLI metric names to JaCoCo counter type names (uppercase).
	typeMap := map[string]string{
		"line":        "LINE",
		"instruction": "INSTRUCTION",
		"branch":      "BRANCH",
		"method":      "METHOD",
		"complexity":  "COMPLEXITY",
	}
	target := typeMap[metricType]
	for _, c := range counters {
		if c.Type == target {
			return c.Missed, c.Covered, true
		}
	}
	return 0, 0, false
}

// parseSonarCoverage parses a Sonar Generic Coverage XML report and returns total coverage,
// per-file/dir coverage, and the count of considered files.
func parseSonarCoverage(reportPath, groupBy string, includeGlobs, excludeGlobs []string) (float64, []packageCoverage, int, error) {
	if groupBy != "file" && groupBy != "dir" {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: unsupported group-by %q; valid: file (class), dir", groupBy)
	}

	raw, err := os.ReadFile(reportPath)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_READ: %w", err)
	}

	var report SonarCoverage
	if err := xml.Unmarshal(raw, &report); err != nil {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_PARSE: %w", err)
	}

	if len(report.Files) == 0 {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: no <file> elements found in Sonar coverage report")
	}

	// Calculate totals and per-group coverage.
	var totalCovered, totalLines int
	byGroup := make(map[string]metricAgg)
	consideredCount := 0

	for _, file := range report.Files {
		if len(file.Lines) == 0 {
			continue
		}

		// Normalize path: extract relative path from absolute path.
		filePath := normalizeSonarPath(file.Path)
		if filePath == "" {
			continue
		}

		// Apply glob filters.
		if len(includeGlobs) > 0 && !matchesAnyGlob(filePath, includeGlobs) {
			continue
		}
		if matchesAnyGlob(filePath, excludeGlobs) {
			continue
		}

		// Count covered and total lines for this file.
		fileCovered := 0
		fileTotal := len(file.Lines)
		for _, line := range file.Lines {
			if line.Covered {
				fileCovered++
			}
		}

		totalCovered += fileCovered
		totalLines += fileTotal

		// Determine group key based on groupBy strategy.
		var groupKey string
		if groupBy == "dir" {
			groupKey = path.Dir(filePath)
			if groupKey == "." || groupKey == "/" {
				groupKey = path.Base(filePath)
			}
		} else {
			groupKey = filePath
		}

		agg := byGroup[groupKey]
		agg.Covered += float64(fileCovered)
		agg.Total += float64(fileTotal)
		byGroup[groupKey] = agg
		consideredCount++
	}

	if totalLines == 0 {
		return 0, nil, 0, fmt.Errorf("ERR_EMPTY_DATASET: no coverage lines found after filtering")
	}

	totalPercent := round2((float64(totalCovered) / float64(totalLines)) * 100)

	pkgs := make([]packageCoverage, 0, len(byGroup))
	for groupKey, agg := range byGroup {
		if agg.Total <= 0 {
			continue
		}
		pct := round2((agg.Covered / agg.Total) * 100)
		pkgs = append(pkgs, packageCoverage{
			ImportPath:      groupKey,
			CoveragePercent: pct,
		})
	}

	if len(pkgs) == 0 {
		return 0, nil, 0, fmt.Errorf("ERR_EMPTY_DATASET: generated packages list is empty")
	}

	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return totalPercent, pkgs, consideredCount, nil
}

// normalizeSonarPath extracts a meaningful relative path from Sonar's absolute file paths.
// It strips common prefixes like DerivedData paths to get a project-relative path.
func normalizeSonarPath(filePath string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(filePath, "\\", "/"))
	if normalized == "" {
		return ""
	}

	// Common iOS/Xcode path patterns to strip.
	patternsToStrip := []string{
		"/SourcePackages/checkouts/",
		// "/DerivedData/",
		// "/Build/Intermediates/",
	}

	for _, pattern := range patternsToStrip {
		if idx := strings.Index(normalized, pattern); idx != -1 {
			// Keep everything after the pattern.
			normalized = normalized[idx+len(pattern):]
			break
		}
	}

	// If path starts with /Users/ or similar, try to extract the last meaningful parts.
	if strings.HasPrefix(normalized, "/Users/") || strings.HasPrefix(normalized, "/home/") {
		// Find Sources/ or src/ or similar common source directories.
		sourceMarkers := []string{"/Sources/", "/src/", "/Source/", "/App/"}
		for _, marker := range sourceMarkers {
			if idx := strings.Index(normalized, marker); idx != -1 {
				normalized = normalized[idx+1:] // Keep the marker's directory name.
				break
			}
		}
	}

	// Remove leading slashes.
	normalized = strings.TrimPrefix(normalized, "/")

	return path.Clean(normalized)
}

func parseVitestSummary(summaryPath, metric, groupBy, pathStripPrefix string, includeGlobs, excludeGlobs []string) (float64, []packageCoverage, int, error) {
	if metric != "lines" && metric != "statements" && metric != "functions" && metric != "branches" {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: unsupported metric %q", metric)
	}
	if groupBy != "dir" && groupBy != "file" {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: unsupported group-by %q", groupBy)
	}

	raw, err := os.ReadFile(summaryPath)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_READ: %w", err)
	}

	entries := map[string]vitestSummaryEntry{}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_PARSE: %w", err)
	}

	totalEntry, ok := entries["total"]
	if !ok {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: total section is required")
	}

	totalMetric, ok := selectVitestMetric(totalEntry, metric)
	if !ok {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: selected metric %q not found in total section", metric)
	}
	if totalMetric.Pct < 0 || totalMetric.Pct > 100 {
		return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: total %s.pct must be between 0 and 100", metric)
	}

	stripPrefix := strings.TrimSpace(pathStripPrefix)
	if stripPrefix == "" {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			stripPrefix = cwd
		}
	}

	byGroup := make(map[string]metricAgg)
	consideredFiles := 0

	for filePath, entry := range entries {
		if filePath == "total" {
			continue
		}

		fileMetric, ok := selectVitestMetric(entry, metric)
		if !ok {
			continue
		}
		if fileMetric.Total <= 0 {
			continue
		}

		normalizedPath := normalizeCoveragePath(filePath, stripPrefix)
		if normalizedPath == "" {
			continue
		}

		if len(includeGlobs) > 0 && !matchesAnyGlob(normalizedPath, includeGlobs) {
			continue
		}
		if matchesAnyGlob(normalizedPath, excludeGlobs) {
			continue
		}

		groupKey := normalizedPath
		if groupBy == "dir" {
			groupKey = path.Dir(normalizedPath)
			if groupKey == "." || groupKey == "/" {
				groupKey = path.Base(normalizedPath)
			}
		}

		agg := byGroup[groupKey]
		agg.Covered += fileMetric.Covered
		agg.Total += fileMetric.Total
		byGroup[groupKey] = agg
		consideredFiles++
	}

	if consideredFiles == 0 || len(byGroup) == 0 {
		return 0, nil, 0, fmt.Errorf("ERR_EMPTY_DATASET: no coverage files remained after filtering")
	}

	pkgs := make([]packageCoverage, 0, len(byGroup))
	for groupKey, agg := range byGroup {
		if agg.Total <= 0 {
			continue
		}
		pct := round2((agg.Covered / agg.Total) * 100)
		if pct < 0 || pct > 100 {
			return 0, nil, 0, fmt.Errorf("ERR_INPUT_SCHEMA: computed package coverage out of range for %q", groupKey)
		}
		pkgs = append(pkgs, packageCoverage{
			ImportPath:      groupKey,
			CoveragePercent: pct,
		})
	}

	if len(pkgs) == 0 {
		return 0, nil, 0, fmt.Errorf("ERR_EMPTY_DATASET: generated packages list is empty")
	}

	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return round2(totalMetric.Pct), pkgs, consideredFiles, nil
}

func selectVitestMetric(entry vitestSummaryEntry, metric string) (vitestMetric, bool) {
	switch metric {
	case "lines":
		return entry.Lines, true
	case "statements":
		return entry.Statements, true
	case "functions":
		return entry.Functions, true
	case "branches":
		return entry.Branches, true
	default:
		return vitestMetric{}, false
	}
}

func normalizeCoveragePath(filePath, stripPrefix string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(filePath, "\\", "/"))
	if normalized == "" {
		return ""
	}

	normalized = path.Clean(normalized)
	if normalized == "." {
		return ""
	}

	if stripPrefix != "" {
		prefix := path.Clean(strings.ReplaceAll(strings.TrimSpace(stripPrefix), "\\", "/"))
		if prefix != "." && prefix != "" {
			trimmed := strings.TrimPrefix(normalized, prefix)
			trimmed = strings.TrimPrefix(trimmed, "/")
			if trimmed != normalized {
				normalized = trimmed
			}
		}
	}

	if len(normalized) >= 2 && normalized[1] == ':' {
		normalized = strings.TrimPrefix(normalized[2:], "/")
	}
	normalized = strings.TrimPrefix(normalized, "/")
	normalized = strings.TrimPrefix(normalized, "./")

	if normalized == "" {
		return ""
	}

	return path.Clean(normalized)
}

func matchesAnyGlob(pathValue string, globs []string) bool {
	for _, glob := range globs {
		if matchGlob(pathValue, glob) {
			return true
		}
	}
	return false
}

func matchGlob(pathValue, glob string) bool {
	pattern := regexp.QuoteMeta(strings.TrimSpace(glob))
	if pattern == "" {
		return false
	}

	pattern = strings.ReplaceAll(pattern, `\*\*`, `.*`)
	pattern = strings.ReplaceAll(pattern, `\*`, `[^/]*`)
	pattern = strings.ReplaceAll(pattern, `\?`, `[^/]`)

	re, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		return false
	}

	return re.MatchString(pathValue)
}

func parseCoverage(profilePath string) (float64, []packageCoverage, error) {
	cmd := exec.Command("go", "tool", "cover", "-func", profilePath)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return 0, nil, fmt.Errorf("go tool cover failed: %s", string(ee.Stderr))
		}
		return 0, nil, err
	}

	lineRe := regexp.MustCompile(`^(.+):[0-9]+:\s+\S+\s+([0-9]+(?:\.[0-9]+)?)%$`)
	totalRe := regexp.MustCompile(`^total:\s+\(statements\)\s+([0-9]+(?:\.[0-9]+)?)%$`)

	type agg struct {
		sum   float64
		count int
	}
	byPackage := map[string]*agg{}
	var total float64
	foundTotal := false

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := totalRe.FindStringSubmatch(line); len(m) == 2 {
			t, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				return 0, nil, fmt.Errorf("parse total coverage: %w", err)
			}
			total = t
			foundTotal = true
			continue
		}

		m := lineRe.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		filePath := m[1]
		percent, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			return 0, nil, fmt.Errorf("parse package coverage: %w", err)
		}
		pkg := path.Dir(filePath)
		if byPackage[pkg] == nil {
			byPackage[pkg] = &agg{}
		}
		byPackage[pkg].sum += percent
		byPackage[pkg].count++
	}

	if !foundTotal {
		return 0, nil, fmt.Errorf("total coverage line not found in cover output")
	}

	pkgs := make([]packageCoverage, 0, len(byPackage))
	for pkg, a := range byPackage {
		if a.count == 0 {
			continue
		}
		pkgs = append(pkgs, packageCoverage{
			ImportPath:      pkg,
			CoveragePercent: round2(a.sum / float64(a.count)),
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })

	return round2(total), pkgs, nil
}

func uploadPayload(url, apiKeyHeader, apiKey string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(apiKeyHeader, apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}

func envOrDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func exitErr(stage string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", stage, err)
	os.Exit(1)
}
