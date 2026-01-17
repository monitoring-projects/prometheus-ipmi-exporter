// Copyright 2021 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/prometheus-community/ipmi_exporter/freeipmi"
	"github.com/prometheus/client_golang/prometheus"
)

// TestCase represents a single IPMI test case
type TestCase struct {
	Name        string
	Description string
	Collector   collector
	Target      string
	Module      string
	Expected    string // What we expect to find (e.g., "sensor data", "chassis info")
}

// TestResult represents the result of a single test case
type TestResult struct {
	TestCase     TestCase
	Passed       bool
	Duration     time.Duration
	Error        error
	Output       string
	Trace        string
	MetricsCount int
	Metrics      []string // Store metric values for debug
}

// TestSuite manages and runs all IPMI tests
type TestSuite struct {
	config     *SafeConfig
	results    []TestResult
	logger     *log.Logger
	slogLogger *slog.Logger
	debug      bool
}

// NewTestSuite creates a new test suite
func NewTestSuite(config *SafeConfig, logger *log.Logger, debug bool) *TestSuite {
	slogLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
	return &TestSuite{
		config:     config,
		results:    make([]TestResult, 0),
		logger:     logger,
		slogLogger: slogLogger,
		debug:      debug,
	}
}

// GetAllTestCases returns all available test cases for both FreeIPMI and Native implementations
func (ts *TestSuite) GetAllTestCases() []TestCase {
	testCases := []TestCase{}

	// FreeIPMI test cases
	if !*nativeIPMI {
		testCases = append(testCases,
			TestCase{
				Name:        "bmc_info",
				Description: "Get BMC device information",
				Collector:   &BMCCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "BMC device info",
			},
			TestCase{
				Name:        "chassis_info",
				Description: "Get chassis information",
				Collector:   &ChassisCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "chassis info",
			},
			TestCase{
				Name:        "dcmi_info",
				Description: "Get DCMI power management information",
				Collector:   &DCMICollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "DCMI power data",
			},
			TestCase{
				Name:        "ipmi_sensor",
				Description: "Get IPMI sensor readings",
				Collector:   &IPMICollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "sensor readings",
			},
			TestCase{
				Name:        "sel_info",
				Description: "Get System Event Log information",
				Collector:   &SELCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "SEL entries",
			},
			TestCase{
				Name:        "sel_events",
				Description: "Get System Event Log events",
				Collector:   &SELEventsCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "SEL events",
			},
			TestCase{
				Name:        "bmc_watchdog",
				Description: "Get BMC watchdog timer information",
				Collector:   &BMCWatchdogCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "watchdog info",
			},
			TestCase{
				Name:        "sm_lan_mode",
				Description: "Get shared memory LAN mode information",
				Collector:   &SMLANModeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "LAN mode data",
			},
		)
	}

	// Native IPMI test cases
	if *nativeIPMI {
		testCases = append(testCases,
			TestCase{
				Name:        "bmc_info_native",
				Description: "Get BMC device information (Native)",
				Collector:   &BMCNativeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "BMC device info",
			},
			TestCase{
				Name:        "chassis_info_native",
				Description: "Get chassis information (Native)",
				Collector:   &ChassisNativeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "chassis info",
			},
			TestCase{
				Name:        "dcmi_info_native",
				Description: "Get DCMI power management information (Native)",
				Collector:   &DCMINativeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "DCMI power data",
			},
			TestCase{
				Name:        "ipmi_sensor_native",
				Description: "Get IPMI sensor readings (Native)",
				Collector:   &IPMINativeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "sensor readings",
			},
			TestCase{
				Name:        "sel_info_native",
				Description: "Get System Event Log information (Native)",
				Collector:   &SELNativeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "SEL entries",
			},
			TestCase{
				Name:        "sel_events_native",
				Description: "Get System Event Log events (Native)",
				Collector:   &SELEventsNativeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "SEL events",
			},
			TestCase{
				Name:        "bmc_watchdog_native",
				Description: "Get BMC watchdog timer information (Native)",
				Collector:   &BMCWatchdogNativeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "watchdog info",
			},
			TestCase{
				Name:        "sm_lan_mode_native",
				Description: "Get shared memory LAN mode information (Native)",
				Collector:   &SMLANModeNativeCollector{},
				Target:      targetLocal,
				Module:      "default",
				Expected:    "LAN mode data",
			},
		)
	}

	return testCases
}

// DebugMetricCollector captures metric values for debugging
type DebugMetricCollector struct {
	metrics []string
}

func (d *DebugMetricCollector) Write(metrics []byte) (int, error) {
	if len(metrics) > 0 {
		d.metrics = append(d.metrics, string(metrics))
	}
	return len(metrics), nil
}

func (d *DebugMetricCollector) String() string {
	return "debug_collector"
}

// RunTest executes a single test case
func (ts *TestSuite) RunTest(testCase TestCase) TestResult {
	start := time.Now()
	result := TestResult{
		TestCase:     testCase,
		Passed:       false,
		Duration:     0,
		Error:        nil,
		Output:       "",
		Trace:        "",
		MetricsCount: 0,
		Metrics:      make([]string, 0),
	}

	ts.logger.Printf("Running test: %s - %s", testCase.Name, testCase.Description)

	// Create a real prometheus metrics channel
	ch := make(chan prometheus.Metric, 100)
	defer close(ch)

	// Create target
	target := ipmiTarget{
		host:   testCase.Target,
		config: IPMIConfig{}, // Use empty config for testing
	}

	// Execute the collector
	var output freeipmi.Result

	// Execute command based on collector type
	if strings.HasSuffix(testCase.Name, "_native") {
		// Native IPMI execution would go here
		// For now, we'll simulate it with a mock result
		output = freeipmi.Result{}
		// We can't set unexported fields, so we'll handle this in the collection phase
	} else {
		// FreeIPMI execution
		cmd := testCase.Collector.Cmd()
		args := testCase.Collector.Args()

		// Execute the command
		output = freeipmi.Execute(cmd, args, "", testCase.Target, ts.slogLogger)
		// Check if there was an error by trying to access the result
		// Since we can't access unexported fields, we'll handle this differently
	}

	// Collect metrics
	metricsCount := 0
	count, collectErr := testCase.Collector.Collect(output, ch, target)
	if collectErr != nil {
		result.Error = collectErr
		result.Trace += fmt.Sprintf("\nCollection failed: %v", collectErr)
	} else {
		metricsCount = count

		// Capture metric values if debug mode is enabled
		if ts.debug {
			for i := 0; i < metricsCount; i++ {
				metric := <-ch
				// Convert metric to string representation
				desc := metric.Desc()
				result.Metrics = append(result.Metrics, fmt.Sprintf("Metric: %s", desc))

				// Create a simple, readable metric summary
				var metricInfo strings.Builder

				// Extract metric name from description
				descStr := desc.String()
				if strings.Contains(descStr, "fqName:") {
					start := strings.Index(descStr, "fqName: \"") + 9
					end := strings.Index(descStr[start:], "\"")
					if end != -1 {
						metricName := descStr[start : start+end]
						metricInfo.WriteString(fmt.Sprintf("Metric Name: %s\n", metricName))
					}
				}

				// Extract help text
				if strings.Contains(descStr, "help:") {
					start := strings.Index(descStr, "help: \"") + 7
					end := strings.Index(descStr[start:], "\"")
					if end != -1 {
						helpText := descStr[start : start+end]
						metricInfo.WriteString(fmt.Sprintf("Description: %s\n", helpText))
					}
				}

				// Extract variable labels
				if strings.Contains(descStr, "variableLabels:") {
					start := strings.Index(descStr, "variableLabels:") + 15
					end := strings.Index(descStr[start:], "}")
					if end != -1 {
						labelsStr := descStr[start : start+end]
						labelsStr = strings.TrimSpace(labelsStr)
						if labelsStr != "" && labelsStr != "{}" {
							metricInfo.WriteString(fmt.Sprintf("Available Labels: %s\n", labelsStr))
						}
					}
				}

				// Show actual IPMI command output for debugging
				metricInfo.WriteString("IPMI Command Output:\n")
				cmd := testCase.Collector.Cmd()
				args := testCase.Collector.Args()
				metricInfo.WriteString(fmt.Sprintf("  Command: %s\n", cmd))
				metricInfo.WriteString(fmt.Sprintf("  Args: %v\n", args))

				// Show the raw FreeIPMI output
				ipmiOutput := freeipmi.Execute(cmd, args, "", testCase.Target, ts.slogLogger)
				metricInfo.WriteString(fmt.Sprintf("  Raw Output: %+v\n", ipmiOutput))

				result.Metrics = append(result.Metrics, metricInfo.String())
			}
		} else {
			// Just drain the channel to prevent deadlock
			for i := 0; i < metricsCount; i++ {
				<-ch
			}
		}
	}

	// Determine if test passed
	result.Passed = result.Error == nil && metricsCount > 0
	result.Duration = time.Since(start)
	result.MetricsCount = metricsCount

	// We can't access the output field directly, so we'll use a placeholder
	result.Output = "test execution completed"

	// Add trace information
	if result.Trace == "" {
		result.Trace = fmt.Sprintf("Command: %s %v\nMetrics collected: %d",
			testCase.Collector.Cmd(), testCase.Collector.Args(), metricsCount)
	}

	return result
}

// RunAllTests executes all test cases
func (ts *TestSuite) RunAllTests() {
	testCases := ts.GetAllTestCases()
	ts.logger.Printf("Starting comprehensive IPMI test suite - total tests: %d", len(testCases))

	for _, testCase := range testCases {
		result := ts.RunTest(testCase)
		ts.results = append(ts.results, result)

		// Log immediate result
		if result.Passed {
			ts.logger.Printf("Test PASSED: %s (duration: %v, metrics: %d)", testCase.Name, result.Duration, result.MetricsCount)
		} else {
			ts.logger.Printf("Test FAILED: %s (duration: %v, error: %v)", testCase.Name, result.Duration, result.Error)
			// Print trace for failed tests
			fmt.Printf("\n=== FAILED TEST TRACE: %s ===\n", testCase.Name)
			fmt.Printf("%s\n", result.Trace)
			fmt.Printf("=== END TRACE ===\n\n")
		}

		// Show debug information if enabled
		if ts.debug && len(result.Metrics) > 0 {
			fmt.Printf("\n=== DEBUG METRICS: %s ===\n", testCase.Name)
			for i, metric := range result.Metrics {
				if strings.HasPrefix(metric, "Metric:") {
					fmt.Printf("%s\n", metric)
				} else {
					// This is the detailed metric info
					fmt.Printf("%s", metric)
					if i < len(result.Metrics)-1 && !strings.HasPrefix(result.Metrics[i+1], "Metric:") {
						fmt.Printf("---\n")
					}
				}
			}
			fmt.Printf("=== END DEBUG METRICS ===\n\n")
		}
	}
}

// PrintResultsTable displays a formatted results table
func (ts *TestSuite) PrintResultsTable() {
	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("%-25s %-35s %-8s %-12s %-10s %-15s\n", "TEST NAME", "DESCRIPTION", "STATUS", "DURATION", "METRICS", "ERROR")
	fmt.Printf("------------------------------------------------------------------------------------------------------------------------\n")

	passed := 0
	failed := 0
	totalDuration := time.Duration(0)

	for _, result := range ts.results {
		status := "FAIL"
		statusColor := "\033[31m" // Red
		if result.Passed {
			status = "PASS"
			statusColor = "\033[32m" // Green
			passed++
		} else {
			failed++
		}

		errorMsg := ""
		if result.Error != nil {
			errorMsg = result.Error.Error()
			if len(errorMsg) > 15 {
				errorMsg = errorMsg[:12] + "..."
			}
		}

		// Truncate description if too long
		description := result.TestCase.Description
		if len(description) > 35 {
			description = description[:32] + "..."
		}

		fmt.Printf("%-25s %-35s %s%-8s\033[0m %-12v %-10d %-15s\n",
			result.TestCase.Name,
			description,
			statusColor, status,
			result.Duration,
			result.MetricsCount,
			errorMsg,
		)

		totalDuration += result.Duration
	}

	fmt.Printf("------------------------------------------------------------------------------------------------------------------------\n")
	fmt.Printf("SUMMARY: %d PASSED, %d FAILED, %d TOTAL\n", passed, failed, len(ts.results))
	fmt.Printf("TOTAL DURATION: %v\n", totalDuration)
	fmt.Printf("IMPLEMENTATION: %s\n", map[bool]string{true: "Native IPMI", false: "FreeIPMI"}[*nativeIPMI])
	fmt.Printf("========================================================================================================================\n")
}

// GetSummary returns a summary of test results
func (ts *TestSuite) GetSummary() (passed, failed, total int, totalDuration time.Duration) {
	passed = 0
	failed = 0
	totalDuration = time.Duration(0)

	for _, result := range ts.results {
		if result.Passed {
			passed++
		} else {
			failed++
		}
		totalDuration += result.Duration
	}

	return passed, failed, len(ts.results), totalDuration
}
