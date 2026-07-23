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
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	yaml "gopkg.in/yaml.v2"
)

// defaultTargetsFile is the Prometheus file_sd-compatible target list also
// used by discoverHandler.
const defaultTargetsFile = "/etc/ipmi-exporter/ipmi-targets.yml"

// targetGroup mirrors a single entry of the Prometheus file_sd-compatible
// ipmi-targets.yml file: a list of hosts sharing a common label set.
type targetGroup struct {
	Targets []string          `yaml:"targets"`
	Labels  map[string]string `yaml:"labels,omitempty"`
}

// JSONFloat is a float64 that serializes NaN/+Inf/-Inf as JSON null.
type JSONFloat float64

func (f JSONFloat) MarshalJSON() ([]byte, error) {
	v := float64(f)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return []byte("null"), nil
	}
	return []byte(strconv.FormatFloat(v, 'f', -1, 64)), nil
}

// Health status levels, ranked by severity (higher = worse).
const (
	healthHealthy  = "healthy"
	healthUnknown  = "unknown"
	healthWarning  = "warning"
	healthCritical = "critical"
)

var healthRank = map[string]int{
	healthHealthy:  0,
	healthUnknown:  1,
	healthWarning:  2,
	healthCritical: 3,
}

func worseHealth(a, b string) string {
	if healthRank[b] > healthRank[a] {
		return b
	}
	return a
}

// sensorReading is a single, joined (value+state) sensor entry in the tree.
type sensorReading struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Type    string     `json:"type,omitempty"`
	Value   *JSONFloat `json:"value,omitempty"`
	RPM     *JSONFloat `json:"rpm,omitempty"`
	Ratio   *JSONFloat `json:"ratio,omitempty"`
	Volts   *JSONFloat `json:"volts,omitempty"`
	Amps    *JSONFloat `json:"amperes,omitempty"`
	Watts   *JSONFloat `json:"watts,omitempty"`
	Celsius *JSONFloat `json:"celsius,omitempty"`
	State   string     `json:"state"`
}

type bmcInfo struct {
	FirmwareRevision      string `json:"firmware_revision,omitempty"`
	ManufacturerID        string `json:"manufacturer_id,omitempty"`
	SystemFirmwareVersion string `json:"system_firmware_version,omitempty"`
	BMCURL                string `json:"bmc_url,omitempty"`
}

type chassisState struct {
	PowerState        *JSONFloat `json:"power_state,omitempty"`
	DriveFaultState   *JSONFloat `json:"drive_fault_state,omitempty"`
	CoolingFaultState *JSONFloat `json:"cooling_fault_state,omitempty"`
}

type dcmiState struct {
	PowerConsumptionWatts *JSONFloat `json:"power_consumption_watts,omitempty"`
}

type selState struct {
	LogsCount             *JSONFloat            `json:"logs_count,omitempty"`
	FreeSpaceBytes        *JSONFloat            `json:"free_space_bytes,omitempty"`
	EventsByState         map[string]*JSONFloat `json:"events_by_state,omitempty"`
	EventsByName          map[string]*JSONFloat `json:"events_by_name,omitempty"`
	EventsLatestTimestamp map[string]*JSONFloat `json:"events_latest_timestamp,omitempty"`
}

type watchdogState struct {
	TimerState                *JSONFloat `json:"timer_state,omitempty"`
	TimerUse                  string     `json:"timer_use,omitempty"`
	LoggingState              *JSONFloat `json:"logging_state,omitempty"`
	TimeoutAction             string     `json:"timeout_action,omitempty"`
	PretimeoutInterrupt       string     `json:"pretimeout_interrupt,omitempty"`
	PretimeoutIntervalSeconds *JSONFloat `json:"pretimeout_interval_seconds,omitempty"`
	InitialCountdownSeconds   *JSONFloat `json:"initial_countdown_seconds,omitempty"`
	CurrentCountdownSeconds   *JSONFloat `json:"current_countdown_seconds,omitempty"`
}

type sensorGroups struct {
	Temperature []*sensorReading `json:"temperature,omitempty"`
	Fan         []*sensorReading `json:"fan,omitempty"`
	Voltage     []*sensorReading `json:"voltage,omitempty"`
	Current     []*sensorReading `json:"current,omitempty"`
	Power       []*sensorReading `json:"power,omitempty"`
	Generic     []*sensorReading `json:"generic,omitempty"`
}

type nodeHealth struct {
	Status string   `json:"status"`
	Issues []string `json:"issues,omitempty"`
}

// NodeState is the full JSON tree for a single IPMI target/BMC node.
type NodeState struct {
	Host                  string            `json:"host"`
	Labels                map[string]string `json:"labels,omitempty"`
	Module                string            `json:"module"`
	Up                    bool              `json:"up"`
	Error                 string            `json:"error,omitempty"`
	ScrapeDurationSeconds JSONFloat         `json:"scrape_duration_seconds"`
	Health                nodeHealth        `json:"health"`
	Collectors            map[string]struct {
		Up bool `json:"up"`
	} `json:"collectors,omitempty"`
	BMC      *bmcInfo       `json:"bmc,omitempty"`
	Chassis  *chassisState  `json:"chassis,omitempty"`
	DCMI     *dcmiState     `json:"dcmi,omitempty"`
	LANMode  *JSONFloat     `json:"lan_mode,omitempty"`
	Watchdog *watchdogState `json:"watchdog,omitempty"`
	SEL      *selState      `json:"sel,omitempty"`
	Sensors  *sensorGroups  `json:"sensors,omitempty"`
}

type clusterSummary struct {
	TotalNodes    int            `json:"total_nodes"`
	NodesUp       int            `json:"nodes_up"`
	NodesDown     int            `json:"nodes_down"`
	OverallHealth string         `json:"overall_health"`
	HealthCounts  map[string]int `json:"health_counts"`
}

// ClusterState is the top-level JSON document returned by /cluster.
type ClusterState struct {
	GeneratedAt           string         `json:"generated_at"`
	ScrapeDurationSeconds JSONFloat      `json:"scrape_duration_seconds"`
	TargetsFile           string         `json:"targets_file"`
	Summary               clusterSummary `json:"summary"`
	Nodes                 []*NodeState   `json:"nodes"`
}

func clusterHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	targetsFile := defaultTargetsFile
	if v := r.URL.Query().Get("targets_file"); v != "" {
		targetsFile = v
	}

	data, err := os.ReadFile(targetsFile)
	if err != nil {
		logger.Error("Failed to read targets file for /cluster", "error", err, "file", targetsFile)
		http.Error(w, "Failed to read targets file", http.StatusInternalServerError)
		return
	}

	var groups []targetGroup
	if err := yaml.Unmarshal(data, &groups); err != nil {
		logger.Error("Failed to parse targets file for /cluster", "error", err, "file", targetsFile)
		http.Error(w, "Failed to parse targets file", http.StatusInternalServerError)
		return
	}

	type job struct {
		host   string
		labels map[string]string
	}
	var jobs []job
	for _, g := range groups {
		for _, t := range g.Targets {
			jobs = append(jobs, job{host: t, labels: g.Labels})
		}
	}

	nodes := make([]*NodeState, len(jobs))

	const maxConcurrency = 16
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, j job) {
			defer wg.Done()
			defer func() { <-sem }()
			nodes[i] = scrapeNode(j.host, j.labels)
		}(i, j)
	}
	wg.Wait()

	cluster := buildClusterState(nodes, targetsFile, time.Since(start).Seconds())

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]*ClusterState{"cluster": cluster}); err != nil {
		logger.Error("Failed to encode /cluster response", "error", err)
	}
}

func scrapeNode(host string, labels map[string]string) *NodeState {
	module := "default"
	if m, ok := labels["module"]; ok && m != "" {
		module = m
	}

	node := &NodeState{
		Host:   host,
		Labels: labels,
		Module: module,
		Collectors: map[string]struct {
			Up bool `json:"up"`
		}{},
	}

	start := time.Now()

	// Reuse the existing remote /metrics handler for each target.
	req := httptest.NewRequest("GET", "/metrics", nil)
	q := req.URL.Query()
	q.Set("target", host)
	q.Set("module", module)
	req.URL.RawQuery = q.Encode()
	rec := httptest.NewRecorder()

	metricsHandler(rec, req)

	node.ScrapeDurationSeconds = JSONFloat(time.Since(start).Seconds())

	if rec.Code != http.StatusOK {
		node.Error = rec.Body.String()
		computeNodeHealth(node)
		return node
	}

	// Parse the Prometheus text exposition response back into DTOs.
	format := expfmt.ResponseFormat(rec.Header())
	if format == expfmt.FmtUnknown {
		format = expfmt.NewFormat(expfmt.TypeTextPlain)
	}

	var families []*dto.MetricFamily
	dec := expfmt.NewDecoder(rec.Body, format)
	for {
		mf := &dto.MetricFamily{}
		if err := dec.Decode(mf); err != nil {
			if err == io.EOF {
				break
			}
			node.Error = "parse metrics: " + err.Error()
			computeNodeHealth(node)
			return node
		}
		families = append(families, mf)
	}

	populateNodeFromFamilies(node, families)
	computeNodeHealth(node)
	return node
}

func populateNodeFromFamilies(node *NodeState, families []*dto.MetricFamily) {
	fanByKey := map[string]*sensorReading{}
	tempByKey := map[string]*sensorReading{}
	voltByKey := map[string]*sensorReading{}
	currByKey := map[string]*sensorReading{}
	powByKey := map[string]*sensorReading{}
	genByKey := map[string]*sensorReading{}

	getOrCreate := func(m map[string]*sensorReading, key, id, name, typ string) *sensorReading {
		if sr, ok := m[key]; ok {
			return sr
		}
		sr := &sensorReading{ID: id, Name: name, Type: typ}
		m[key] = sr
		return sr
	}

	sel := &selState{
		EventsByState:         map[string]*JSONFloat{},
		EventsByName:          map[string]*JSONFloat{},
		EventsLatestTimestamp: map[string]*JSONFloat{},
	}
	selPopulated := false
	chassis := &chassisState{}
	chassisPopulated := false
	dcmi := &dcmiState{}
	dcmiPopulated := false
	wd := &watchdogState{}
	wdPopulated := false
	var bmc *bmcInfo

	for _, fam := range families {
		name := fam.GetName()
		for _, m := range fam.Metric {
			lbl := map[string]string{}
			for _, l := range m.Label {
				lbl[l.GetName()] = l.GetValue()
			}
			val := 0.0
			if m.Gauge != nil {
				val = m.Gauge.GetValue()
			} else if m.Untyped != nil {
				val = m.Untyped.GetValue()
			} else if m.Counter != nil {
				val = m.Counter.GetValue()
			}
			v := val

			switch name {
			case "ipmi_up":
				node.Up = node.Up || v == 1
				node.Collectors[lbl["collector"]] = struct {
					Up bool `json:"up"`
				}{Up: v == 1}
			case "ipmi_scrape_duration_seconds":
				// already tracked wall-clock separately; ignore internal value
			case "ipmi_bmc_info":
				bmc = &bmcInfo{
					FirmwareRevision:      lbl["firmware_revision"],
					ManufacturerID:        lbl["manufacturer_id"],
					SystemFirmwareVersion: lbl["system_firmware_version"],
					BMCURL:                lbl["bmc_url"],
				}
			case "ipmi_chassis_power_state":
				chassis.PowerState = finiteMetricValue(v)
				chassisPopulated = true
			case "ipmi_chassis_drive_fault_state":
				chassis.DriveFaultState = finiteMetricValue(v)
				chassisPopulated = true
			case "ipmi_chassis_cooling_fault_state":
				chassis.CoolingFaultState = finiteMetricValue(v)
				chassisPopulated = true
			case "ipmi_dcmi_power_consumption_watts":
				dcmi.PowerConsumptionWatts = finiteMetricValue(v)
				dcmiPopulated = true
			case "ipmi_config_lan_mode":
				node.LANMode = finiteMetricValue(v)
			case "ipmi_sel_logs_count":
				sel.LogsCount = finiteMetricValue(v)
				selPopulated = true
			case "ipmi_sel_free_space_bytes":
				sel.FreeSpaceBytes = finiteMetricValue(v)
				selPopulated = true
			case "ipmi_sel_events_count_by_state":
				sel.EventsByState[lbl["state"]] = finiteMetricValue(v)
				selPopulated = true
			case "ipmi_sel_events_count_by_name":
				sel.EventsByName[lbl["name"]] = finiteMetricValue(v)
				selPopulated = true
			case "ipmi_sel_events_latest_timestamp":
				sel.EventsLatestTimestamp[lbl["name"]] = finiteMetricValue(v)
				selPopulated = true
			case "ipmi_bmc_watchdog_timer_state":
				wd.TimerState = finiteMetricValue(v)
				wdPopulated = true
			case "ipmi_bmc_watchdog_timer_use_state":
				if v == 1 {
					wd.TimerUse = lbl["name"]
				}
				wdPopulated = true
			case "ipmi_bmc_watchdog_logging_state":
				wd.LoggingState = finiteMetricValue(v)
				wdPopulated = true
			case "ipmi_bmc_watchdog_timeout_action_state":
				if v == 1 {
					wd.TimeoutAction = lbl["action"]
				}
				wdPopulated = true
			case "ipmi_bmc_watchdog_pretimeout_interrupt_state":
				if v == 1 {
					wd.PretimeoutInterrupt = lbl["interrupt"]
				}
				wdPopulated = true
			case "ipmi_bmc_watchdog_pretimeout_interval_seconds":
				wd.PretimeoutIntervalSeconds = finiteMetricValue(v)
				wdPopulated = true
			case "ipmi_bmc_watchdog_initial_countdown_seconds":
				wd.InitialCountdownSeconds = finiteMetricValue(v)
				wdPopulated = true
			case "ipmi_bmc_watchdog_current_countdown_seconds":
				wd.CurrentCountdownSeconds = finiteMetricValue(v)
				wdPopulated = true

			case "ipmi_temperature_celsius":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(tempByKey, key, lbl["id"], lbl["name"], "")
				sr.Celsius = finiteMetricValue(v)
			case "ipmi_temperature_state":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(tempByKey, key, lbl["id"], lbl["name"], "")
				sr.State = sensorStateLabel(v)

			case "ipmi_fan_speed_rpm":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(fanByKey, key, lbl["id"], lbl["name"], "")
				sr.RPM = finiteMetricValue(v)
			case "ipmi_fan_speed_ratio":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(fanByKey, key, lbl["id"], lbl["name"], "")
				sr.Ratio = finiteMetricValue(v)
			case "ipmi_fan_speed_state":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(fanByKey, key, lbl["id"], lbl["name"], "")
				sr.State = sensorStateLabel(v)

			case "ipmi_voltage_volts":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(voltByKey, key, lbl["id"], lbl["name"], "")
				sr.Volts = finiteMetricValue(v)
			case "ipmi_voltage_state":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(voltByKey, key, lbl["id"], lbl["name"], "")
				sr.State = sensorStateLabel(v)

			case "ipmi_current_amperes":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(currByKey, key, lbl["id"], lbl["name"], "")
				sr.Amps = finiteMetricValue(v)
			case "ipmi_current_state":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(currByKey, key, lbl["id"], lbl["name"], "")
				sr.State = sensorStateLabel(v)

			case "ipmi_power_watts":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(powByKey, key, lbl["id"], lbl["name"], "")
				sr.Watts = finiteMetricValue(v)
			case "ipmi_power_state":
				key := lbl["id"] + "|" + lbl["name"]
				sr := getOrCreate(powByKey, key, lbl["id"], lbl["name"], "")
				sr.State = sensorStateLabel(v)

			case "ipmi_sensor_value":
				key := lbl["id"] + "|" + lbl["name"] + "|" + lbl["type"]
				sr := getOrCreate(genByKey, key, lbl["id"], lbl["name"], lbl["type"])
				sr.Value = finiteMetricValue(v)
			case "ipmi_sensor_state":
				key := lbl["id"] + "|" + lbl["name"] + "|" + lbl["type"]
				sr := getOrCreate(genByKey, key, lbl["id"], lbl["name"], lbl["type"])
				sr.State = sensorStateLabel(v)
			}
		}
	}

	node.BMC = bmc
	if chassisPopulated {
		node.Chassis = chassis
	}
	if dcmiPopulated {
		node.DCMI = dcmi
	}
	if wdPopulated {
		node.Watchdog = wd
	}
	if selPopulated {
		node.SEL = sel
	}

	sensors := &sensorGroups{
		Temperature: sortedReadings(tempByKey),
		Fan:         sortedReadings(fanByKey),
		Voltage:     sortedReadings(voltByKey),
		Current:     sortedReadings(currByKey),
		Power:       sortedReadings(powByKey),
		Generic:     sortedReadings(genByKey),
	}
	if len(sensors.Temperature)+len(sensors.Fan)+len(sensors.Voltage)+len(sensors.Current)+len(sensors.Power)+len(sensors.Generic) > 0 {
		node.Sensors = sensors
	}
}

func finiteMetricValue(v float64) *JSONFloat {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	f := JSONFloat(v)
	return &f
}

func sortedReadings(m map[string]*sensorReading) []*sensorReading {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*sensorReading, 0, len(m))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func sensorStateLabel(v float64) string {
	if math.IsNaN(v) {
		return healthUnknown
	}
	switch v {
	case 0:
		return healthHealthy
	case 1:
		return healthWarning
	case 2:
		return healthCritical
	default:
		return healthUnknown
	}
}

func computeNodeHealth(node *NodeState) {
	status := healthHealthy
	var issues []string

	if !node.Up {
		status = worseHealth(status, healthCritical)
		issues = append(issues, "node is not reachable / scrape failed")
	}

	for name, c := range node.Collectors {
		if !c.Up {
			status = worseHealth(status, healthWarning)
			issues = append(issues, "collector '"+name+"' reported up=0")
		}
	}

	if node.Chassis != nil {
		if node.Chassis.DriveFaultState != nil && *node.Chassis.DriveFaultState == 0 {
			status = worseHealth(status, healthCritical)
			issues = append(issues, "chassis drive fault detected")
		}
		if node.Chassis.CoolingFaultState != nil && *node.Chassis.CoolingFaultState == 0 {
			status = worseHealth(status, healthCritical)
			issues = append(issues, "chassis cooling/fan fault detected")
		}
	}

	checkGroup := func(kind string, readings []*sensorReading) {
		for _, r := range readings {
			switch r.State {
			case healthCritical:
				status = worseHealth(status, healthCritical)
				issues = append(issues, kind+" sensor '"+r.Name+"' is in critical state")
			case healthWarning:
				status = worseHealth(status, healthWarning)
				issues = append(issues, kind+" sensor '"+r.Name+"' is in warning state")
			case healthUnknown:
				status = worseHealth(status, healthUnknown)
			}
		}
	}
	if node.Sensors != nil {
		checkGroup("temperature", node.Sensors.Temperature)
		checkGroup("fan", node.Sensors.Fan)
		checkGroup("voltage", node.Sensors.Voltage)
		checkGroup("current", node.Sensors.Current)
		checkGroup("power", node.Sensors.Power)
		checkGroup("generic", node.Sensors.Generic)
	}

	node.Health = nodeHealth{Status: status, Issues: issues}
}

func buildClusterState(nodes []*NodeState, targetsFile string, duration float64) *ClusterState {
	summary := clusterSummary{
		TotalNodes:    len(nodes),
		OverallHealth: healthHealthy,
		HealthCounts:  map[string]int{healthHealthy: 0, healthWarning: 0, healthCritical: 0, healthUnknown: 0},
	}
	for _, n := range nodes {
		if n.Up {
			summary.NodesUp++
		} else {
			summary.NodesDown++
		}
		summary.HealthCounts[n.Health.Status]++
		summary.OverallHealth = worseHealth(summary.OverallHealth, n.Health.Status)
	}
	return &ClusterState{
		GeneratedAt:           time.Now().Format(time.RFC3339),
		ScrapeDurationSeconds: JSONFloat(duration),
		TargetsFile:           targetsFile,
		Summary:               summary,
		Nodes:                 nodes,
	}
}
