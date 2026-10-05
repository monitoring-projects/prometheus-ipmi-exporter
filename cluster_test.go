package main

import (
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestPopulateFromSampleExposition(t *testing.T) {
	sample := `# HELP ipmi_bmc_info Constant metric with value '1' providing details about the BMC.
# TYPE ipmi_bmc_info gauge
ipmi_bmc_info{bmc_url="N/A",firmware_revision="1.74",manufacturer_id="Super Micro Computer Inc. (10876)",system_firmware_version="N/A"} 1
# HELP ipmi_chassis_power_state Current power state (1=on, 0=off).
# TYPE ipmi_chassis_power_state gauge
ipmi_chassis_power_state 1
# HELP ipmi_fan_speed_rpm Fan speed in rotations per minute.
# TYPE ipmi_fan_speed_rpm gauge
ipmi_fan_speed_rpm{id="1612",name="FAN1"} 4300
# HELP ipmi_fan_speed_state Reported state of a fan speed sensor (0=nominal, 1=warning, 2=critical).
# TYPE ipmi_fan_speed_state gauge
ipmi_fan_speed_state{id="1612",name="FAN1"} 0
# HELP ipmi_sensor_value Generic data read from an IPMI sensor of unknown type, relying on labels for context.
# TYPE ipmi_sensor_value gauge
ipmi_sensor_value{id="2215",name="VBAT",type="Battery"} NaN
# HELP ipmi_sensor_state Indicates the severity of the state reported by an IPMI sensor (0=nominal, 1=warning, 2=critical).
# TYPE ipmi_sensor_state gauge
ipmi_sensor_state{id="2215",name="VBAT",type="Battery"} 0
# HELP ipmi_up '1' if a scrape of the IPMI device was successful, '0' otherwise.
# TYPE ipmi_up gauge
ipmi_up{collector="bmc"} 1
ipmi_up{collector="chassis"} 1
ipmi_up{collector="ipmi"} 1
ipmi_up{collector="sel"} 1
ipmi_up{collector="sel-events"} 1
`

	dec := expfmt.NewDecoder(strings.NewReader(sample), expfmt.NewFormat(expfmt.TypeTextPlain))
	var families []*dto.MetricFamily
	for {
		mf := &dto.MetricFamily{}
		if err := dec.Decode(mf); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode error: %v", err)
		}
		families = append(families, mf)
	}

	node := &NodeState{
		Host:   "192.168.98.1",
		Module: "default",
		Collectors: map[string]struct {
			Up bool `json:"up"`
		}{},
	}
	populateNodeFromFamilies(node, families)
	computeNodeHealth(node)

	b, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("json marshal error: %v", err)
	}
	if !json.Valid(b) {
		t.Fatal("output is not valid json")
	}
}

func TestJSONFloat(t *testing.T) {
	tests := []struct {
		val      float64
		expected string
	}{
		{val: math.NaN(), expected: "null"},
		{val: math.Inf(1), expected: "null"},
		{val: math.Inf(-1), expected: "null"},
		{val: 0.0, expected: "0"},
		{val: 42.5, expected: "42.5"},
	}

	for _, tc := range tests {
		jf := JSONFloat(tc.val)
		b, err := json.Marshal(jf)
		if err != nil {
			t.Fatalf("marshal error for %v: %v", tc.val, err)
		}
		if string(b) != tc.expected {
			t.Errorf("expected %s, got %s", tc.expected, string(b))
		}
	}
}

func TestClusterStateJSONEncodingWithNaN(t *testing.T) {
	nanVal := JSONFloat(math.NaN())
	node := &NodeState{
		Host:                  "192.168.98.1",
		Module:                "default",
		Up:                    false,
		Error:                 "scrape failed",
		ScrapeDurationSeconds: nanVal,
		Health: nodeHealth{
			Status: healthCritical,
			Issues: []string{"node unreachable"},
		},
		Collectors: map[string]struct {
			Up bool `json:"up"`
		}{
			"bmc": {Up: false},
		},
		Sensors: &sensorGroups{
			Generic: []*sensorReading{
				{
					ID:    "2215",
					Name:  "VBAT",
					Type:  "Battery",
					Value: nil,
					State: healthUnknown,
				},
			},
		},
	}

	cluster := buildClusterState([]*NodeState{node}, defaultTargetsFile, math.NaN())
	wrapped := map[string]*ClusterState{"cluster": cluster}

	b, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("cluster marshal error: %v", err)
	}
	if !json.Valid(b) {
		t.Fatal("cluster output is not valid json")
	}
}

func TestIsRemoteMode(t *testing.T) {
	origMode := *configMode
	origFile := *configFile
	defer func() {
		*configMode = origMode
		*configFile = origFile
	}()

	*configMode = "remote"
	*configFile = ""
	if !isRemoteMode() {
		t.Errorf("expected remote mode when configMode=remote")
	}

	*configMode = "local"
	if isRemoteMode() {
		t.Errorf("expected local mode when configMode=local")
	}

	*configMode = ""
	*configFile = "/etc/ipmi-exporter/ipmi-remote.yml"
	if !isRemoteMode() {
		t.Errorf("expected remote mode when configFile contains remote")
	}

	*configMode = ""
	*configFile = "/etc/ipmi-exporter/ipmi-local.yml"
	if isRemoteMode() {
		t.Errorf("expected not remote mode when configFile is local")
	}
}
