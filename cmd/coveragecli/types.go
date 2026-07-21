package main

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// globList is a flag.Value implementation for repeatable glob patterns.
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

// packageCoverage represents coverage data for a single package.
type packageCoverage struct {
	ImportPath      string  `json:"importPath"`
	CoveragePercent float64 `json:"coveragePercent"`
}

// ingestPayload is the API payload for coverage run ingestion.
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

// integrationPayload is the API payload for integration test run ingestion.
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

// e2ePayload is the API payload for E2E test run ingestion.
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

// uploadResponse is the API response structure for uploads.
type uploadResponse struct {
	Run struct {
		Status          string  `json:"status"`
		PassRatePercent float64 `json:"passRatePercent"`
	} `json:"run"`
	Comparison struct {
		DeltaPercent *float64 `json:"deltaPercent"`
	} `json:"comparison"`
}

// vitestMetric represents a single coverage metric from Vitest summary.
type vitestMetric struct {
	Total   float64 `json:"total"`
	Covered float64 `json:"covered"`
	Skipped float64 `json:"skipped"`
	Pct     float64 `json:"pct"`
}

// vitestSummaryEntry represents a file entry in Vitest coverage summary.
type vitestSummaryEntry struct {
	Lines      vitestMetric `json:"lines"`
	Statements vitestMetric `json:"statements"`
	Functions  vitestMetric `json:"functions"`
	Branches   vitestMetric `json:"branches"`
}

// metricAgg is used for aggregating coverage metrics.
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

// JUnitTestCase represents a <testcase> element in JUnit XML.
type JUnitTestCase struct {
	Classname string        `xml:"classname,attr,omitempty"`
	Name      string        `xml:"name,attr"`
	Time      float64       `xml:"time,attr,omitempty"`
	Status    string        `xml:"status,attr,omitempty"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
}

// JUnitFailure represents a <failure> element in JUnit XML.
type JUnitFailure struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

// JUnitSkipped represents a <skipped> element in JUnit XML.
type JUnitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// JUnitProperty represents a <property> element in JUnit XML.
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
