# Mobile Coverage

This guide shows how to configure GitHub Actions to upload mobile unit test report to opencoverage.

## Supported Formats

 Platform | Format 
----------|--------
 Android  | JaCoCo XML 
 iOS      | Sonar Generic XML 

## Prerequisites

- Self-hosted coverage-api instance running (see [SELF_HOSTING.md](SELF_HOSTING.md))
- GitHub repository with mobile code
- Repository secrets configured

## Android Coverage (JaCoCo)

Android unit tests typically produce JaCoCo XML reports via Gradle.

### Report Format

JaCoCo reports contain package-level and class-level coverage counters:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE report PUBLIC "-//JACOCO//DTD Report 1.1//EN" "report.dtd">
<report name="MyApp">
   <package name="com/example/api">
      <class name="com/example/api/Handler" sourcefilename="Handler.kt">
         <counter type="LINE" missed="5" covered="15" />
         <counter type="BRANCH" missed="2" covered="8" />
         <counter type="METHOD" missed="1" covered="4" />
      </class>
      <counter type="LINE" missed="5" covered="15" />
   </package>
</report>
```

### Generate JaCoCo Report

### GitHub Actions Workflow (Android)

```yaml
steps:
    - name: Install coverage CLI
      run: go install github.com/arxdsilva/opencoverage/cmd/coveragecli@latest

    - name: Upload coverage to API
      if: ${{ secrets.COVERAGE_API_URL != '' && secrets.COVERAGE_API_KEY != '' }}
      env:
        COVERAGE_API_URL: ${{ secrets.COVERAGE_API_URL }}
        COVERAGE_API_KEY: ${{ secrets.COVERAGE_API_KEY }}
      run: |
        PROJECT_KEY="${COVERAGE_PROJECT_KEY:-${{ github.repository }}}"
        PROJECT_NAME="${COVERAGE_PROJECT_NAME:-${{ github.event.repository.name }}}"
        PROJECT_GROUP="${COVERAGE_PROJECT_GROUP:-mobile}"

        coveragecli mobile-coverage \
        -report jacoco-report.xml \
        -format jacoco \
        -api-url "$COVERAGE_API_URL" \
        -api-key "$COVERAGE_API_KEY" \
        -project-key "$PROJECT_KEY" \
        -project-name "$PROJECT_NAME" \
        -project-group "$PROJECT_GROUP" \
        -default-branch "main" \
        -branch "${{ github.ref_name }}" \
        -commit-sha "${{ github.sha }}" \
        -author "${{ github.actor }}" \
        -trigger-type "push" \
        -metric "line" \
        -threshold 60
```

---

## iOS Coverage (Sonar Generic XML)

iOS unit tests produce XCResult bundles which must be converted to Sonar Generic XML format.

### Report Format

Sonar Generic XML reports contain file-level line coverage:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<coverage version="1">
   <file path="Sources/App/Services/AuthService.swift">
      <lineToCover lineNumber="10" covered="true"/>
      <lineToCover lineNumber="11" covered="true"/>
      <lineToCover lineNumber="15" covered="false"/>
   </file>
   <file path="Sources/App/Controllers/Controller.swift">
      <lineToCover lineNumber="8" covered="true"/>
      <lineToCover lineNumber="12" covered="false"/>
   </file>
</coverage>
```

### Generate Sonar Generic XML from XCResult

### GitHub Actions Workflow (iOS)

```yaml
steps:
    - name: Install coverage CLI
      run: go install github.com/arxdsilva/opencoverage/cmd/coveragecli@latest

    - name: Upload coverage to API
      if: ${{ secrets.COVERAGE_API_URL != '' && secrets.COVERAGE_API_KEY != '' }}
      env:
        COVERAGE_API_URL: ${{ secrets.COVERAGE_API_URL }}
        COVERAGE_API_KEY: ${{ secrets.COVERAGE_API_KEY }}
      run: |
        PROJECT_KEY="${COVERAGE_PROJECT_KEY:-${{ github.repository }}}"
        PROJECT_NAME="${COVERAGE_PROJECT_NAME:-${{ github.event.repository.name }}}"
        PROJECT_GROUP="${COVERAGE_PROJECT_GROUP:-mobile}"

        coveragecli mobile-coverage \
        -report sonar-coverage.xml \
        -format sonar \
        -api-url "$COVERAGE_API_URL" \
        -api-key "$COVERAGE_API_KEY" \
        -project-key "$PROJECT_KEY" \
        -project-name "$PROJECT_NAME" \
        -project-group "$PROJECT_GROUP" \
        -default-branch "main" \
        -branch "${{ github.ref_name }}" \
        -commit-sha "${{ github.sha }}" \
        -author "${{ github.actor }}" \
        -trigger-type "push" \
        -threshold 60
```

---

## Advanced Configuration

### Filtering Packages/Files

Use glob patterns to include or exclude specific packages/file:

```bash
# Exclude generated gRPC code
coveragecli mobile-coverage \
  -report coverage.xml \
  -format sonar \
  -exclude-glob "**/Generated/**" \

```

### Grouping Strategy

Control how coverage is grouped in the report:

```bash
# Group by directory (default)
coveragecli mobile-coverage 
  -report coverage.xml \
  -format jacoco \
  -group-by dir

# Group by individual file
coveragecli mobile-coverage 
  -report coverage.xml \
  -format sonar \
  -group-by file
```

---

## CLI Reference

### mobile-coverage Command

| Flag | Description | Default |
|------|-------------|---------|
| `-report` | Path to coverage report (required) | - |
| `-format` | Report format: `jacoco` or `sonar` | `jacoco` |
| `-api-url` | Coverage API URL | `$API_URL` or `http://localhost:8080/v1/coverage-runs` |
| `-api-key` | API key for authentication | `$API_KEY` |
| `-api-key-header` | Custom API key header name | `X-API-Key` |
| `-project-key` | Unique project identifier | `$COVERAGE_PROJECT_KEY` or repository |
| `-project-name` | Display name for the project | `$COVERAGE_PROJECT_NAME` or `coverage-api` |
| `-project-group` | Optional project group | - |
| `-default-branch` | Default branch name | `main` |
| `-branch` | Current branch name | `main` |
| `-commit-sha` | Commit SHA | `local` |
| `-author` | Author of the commit | `local` |
| `-trigger-type` | Trigger type: `push`, `pr`, or `manual` | `manual` |
| `-run-timestamp` | Run timestamp (RFC3339) | current time |
| `-threshold` | Custom threshold percentage | 0 (disabled) |
| `-metric` | Coverage metric (JaCoCo only) | `line` |
| `-group-by` | Grouping strategy: `dir` or `file` | `dir` |
| `-include-glob` | Include packages matching glob (repeatable) | - |
| `-exclude-glob` | Exclude packages matching glob (repeatable) | - |
| `-out` | Path to write generated payload | - |
| `-dry-run` | Generate payload without uploading | `false` |

