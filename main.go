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
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	kingpin "github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	"gopkg.in/yaml.v2"
)

var (
	configFile = kingpin.Flag(
		"config.file",
		"Path to configuration file.",
	).String()
	configMode = kingpin.Flag(
		"config.mode",
		"Configuration mode: local, remote, local-sudo, or custom. Automatically selects appropriate config file.",
	).Enum("local", "remote", "local-sudo", "custom")
	executablesPath = kingpin.Flag(
		"freeipmi.path",
		"Path to FreeIPMI executables (default: rely on $PATH).",
	).String()
	nativeIPMI = kingpin.Flag(
		"native-ipmi",
		"Use native IPMI implementation instead of FreeIPMI (EXPERIMENTAL)",
	).Bool()
	testMode = kingpin.Flag(
		"test",
		"Run comprehensive IPMI tests to validate all implementations and show results table",
	).Bool()
	testDebug = kingpin.Flag(
		"test.debug",
		"Show detailed debug information including metric values during testing",
	).Bool()
	webConfig = webflag.AddFlags(kingpin.CommandLine, ":9290")

	sc = &SafeConfig{
		C: &Config{},
	}
	reloadCh chan chan error

	logger *slog.Logger
)

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")

	// If target parameter is provided, return remote IPMI metrics
	if target != "" {
		// Remote scrape will not work without some kind of config, so be pedantic about it
		module := r.URL.Query().Get("module")
		if module == "" {
			module = "default"
		}
		if !sc.HasModule(module) {
			http.Error(w, fmt.Sprintf("Unknown module %q", module), http.StatusBadRequest)
			return
		}

		logger.Debug("Scraping remote target via metrics endpoint", "target", target, "module", module)

		registry := prometheus.NewRegistry()
		remoteCollector := metaCollector{target: target, module: module, config: sc}
		registry.MustRegister(remoteCollector)
		h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
		h.ServeHTTP(w, r)
		return
	}

	// If no target parameter, return local metrics using the default handler
	promhttp.Handler().ServeHTTP(w, r)
}

func remoteIPMIHandler(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "'target' parameter must be specified", 400)
		return
	}

	// Remote scrape will not work without some kind of config, so be pedantic about it
	module := r.URL.Query().Get("module")
	if module == "" {
		module = "default"
	}
	if !sc.HasModule(module) {
		http.Error(w, fmt.Sprintf("Unknown module %q", module), http.StatusBadRequest)
		return
	}

	logger.Debug("Scraping target via legacy /ipmi endpoint", "target", target, "module", module)

	registry := prometheus.NewRegistry()
	remoteCollector := metaCollector{target: target, module: module, config: sc}
	registry.MustRegister(remoteCollector)
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

func discoverHandler(w http.ResponseWriter, _ *http.Request) {
	// Path to ipmi-targets.yml file
	targetsFile := "/etc/ipmi-exporter/ipmi-targets.yml"

	// Read the YAML file
	data, err := os.ReadFile(targetsFile)
	if err != nil {
		logger.Error("Failed to read ipmi-targets.yml", "error", err)
		http.Error(w, "Failed to read targets file", http.StatusInternalServerError)
		return
	}

	// Parse YAML content - it's a list of target groups
	var targetGroups []struct {
		Targets []string          `yaml:"targets"`
		Labels  map[string]string `yaml:"labels,omitempty"`
	}

	err = yaml.Unmarshal(data, &targetGroups)
	if err != nil {
		logger.Error("Failed to parse ipmi-targets.yml", "error", err)
		http.Error(w, "Failed to parse targets file", http.StatusInternalServerError)
		return
	}

	// Convert to the required JSON format
	var result []map[string][]string
	for _, group := range targetGroups {
		for _, target := range group.Targets {
			result = append(result, map[string][]string{"targets": {target}})
		}
	}

	// Set content type and return JSON response
	w.Header().Set("Content-Type", "application/json")
	jsonData, err := json.Marshal(result)
	if err != nil {
		logger.Error("Failed to marshal JSON response", "error", err)
		http.Error(w, "Failed to create response", http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(jsonData); err != nil {
		logger.Error("Failed to write response", "error", err)
	}
}

func updateConfiguration(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		rc := make(chan error)
		reloadCh <- rc
		if err := <-rc; err != nil {
			http.Error(w, fmt.Sprintf("failed to reload config: %s", err), http.StatusInternalServerError)
		}
	default:
		logger.Error("Only POST requests allowed", "url", r.URL)
		w.Header().Set("Allow", "POST")
		http.Error(w, "Only POST requests allowed", http.StatusMethodNotAllowed)
	}
}

func main() {
	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.CommandLine.UsageWriter(os.Stdout)
	kingpin.HelpFlag.Short('h')
	kingpin.Version(version.Print("ipmi_exporter"))
	kingpin.Parse()
	logger = promslog.New(promslogConfig)
	logger.Info("Starting ipmi_exporter", "version", version.Info())
	if *nativeIPMI {
		logger.Info("Using Go-native IPMI implementation - this is currently EXPERIMENTAL")
		logger.Info("Make sure to read https://github.com/monitoring-projects/ipmi_exporter/blob/master/docs/native.md")
	}

	// Handle configuration mode selection
	if *configMode != "" && *configFile == "" {
		switch *configMode {
		case "local":
			*configFile = "/etc/ipmi-exporter/ipmi-local.yml"
			logger.Info("Using local configuration mode", "config", *configFile)
		case "remote":
			*configFile = "/etc/ipmi-exporter/ipmi-remote.yml"
			logger.Info("Using remote configuration mode", "config", *configFile)
		case "local-sudo":
			*configFile = "/etc/ipmi-exporter/ipmi-local-sudo.yml"
			logger.Info("Using local-sudo configuration mode", "config", *configFile)
		case "custom":
			*configFile = "/etc/ipmi-exporter/ipmi-custom.yml"
			logger.Info("Using custom configuration mode", "config", *configFile)
		}
	} else if *configMode != "" && *configFile != "" {
		logger.Info("Using specified configuration file", "mode", *configMode, "config", *configFile)
	} else if *configFile == "" {
		// Default to local mode if no config specified
		*configFile = "/etc/ipmi-exporter/ipmi-local.yml"
		logger.Info("No configuration specified, using default local mode", "config", *configFile)
	}

	// Check if test mode is enabled
	if *testMode {
		logger.Info("Running in test mode - validating all IPMI implementations")

		// Bail early if the config is bad.
		if err := sc.ReloadConfig(*configFile); err != nil {
			logger.Error("Error parsing config file", "error", err)
			os.Exit(1)
		}

		// Create a standard logger for the test suite
		testLogger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)

		// Create and run test suite
		testSuite := NewTestSuite(sc, testLogger, *testDebug)
		testSuite.RunAllTests()

		// Print results table
		testSuite.PrintResultsTable()

		// Get summary and exit with appropriate code
		passed, failed, total, _ := testSuite.GetSummary()
		logger.Info("Test suite completed", "passed", passed, "failed", failed, "total", total)

		if failed > 0 {
			logger.Error("Some tests failed - see output above for details")
			os.Exit(1)
		}
		logger.Info("All tests passed successfully!")
		os.Exit(0)
	}

	// Bail early if the config is bad.
	if err := sc.ReloadConfig(*configFile); err != nil {
		logger.Error("Error parsing config file", "error", err)
		os.Exit(1)
	}

	hup := make(chan os.Signal, 1)
	reloadCh = make(chan chan error)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-hup:
				if err := sc.ReloadConfig(*configFile); err != nil {
					logger.Error("Error reloading config", "error", err)
				}
			case rc := <-reloadCh:
				if err := sc.ReloadConfig(*configFile); err != nil {
					logger.Error("Error reloading config", "error", err)
					rc <- err
				} else {
					rc <- nil
				}
			}
		}
	}()

	prometheus.MustRegister(versioncollector.NewCollector("ipmi_exporter"))
	localCollector := metaCollector{target: targetLocal, module: "default", config: sc}
	prometheus.MustRegister(&localCollector)

	http.HandleFunc("/metrics", metricsHandler)       // Enhanced metrics endpoint supporting target parameter.
	http.HandleFunc("/ipmi", remoteIPMIHandler)       // Legacy endpoint for IPMI scrapes (backward compatibility).
	http.HandleFunc("/discover", discoverHandler)     // Endpoint to discover IPMI targets.
	http.HandleFunc("/-/reload", updateConfiguration) // Endpoint to reload configuration.

	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>
            <head>
            <title>IPMI Exporter</title>
            <style>
            label{
            display:inline-block;
            width:75px;
            }
            form label {
            margin: 10px;
            }
            form input {
            margin: 10px;
            }
            </style>
            </head>
            <body>
            <h1>IPMI Exporter</h1>
            <form action="/ipmi">
            <label>Target:</label> <input type="text" name="target" placeholder="X.X.X.X" value="1.2.3.4"><br>
            <input type="submit" value="Submit">
			</form>
			<p><a href="/metrics">Local metrics</a></p>
			<p><a href="/config">Config</a></p>
            </body>
            </html>`))
	})

	srv := &http.Server{}
	if err := web.ListenAndServe(srv, webConfig, logger); err != nil {
		logger.Error("HTTP listener stopped", "error", err)
		os.Exit(1)
	}
}
