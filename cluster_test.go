package main

import (
	"encoding/json"
	"io"
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
