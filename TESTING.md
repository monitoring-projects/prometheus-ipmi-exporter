# IPMI Exporter Testing

This document describes the comprehensive testing capabilities built into the IPMI exporter.

## Test Mode

The IPMI exporter includes a comprehensive test mode that validates all IPMI implementations and provides detailed results.

### Usage

```bash
# Test FreeIPMI implementation
./ipmi_exporter --test

# Test Native IPMI implementation
./ipmi_exporter --test --native-ipmi

# Test with debug output (shows metric values)
./ipmi_exporter --test --test.debug

# Test Native IPMI implementation with debug
./ipmi_exporter --test --native-ipmi --test.debug

# Test with custom config
./ipmi_exporter --test --config.file=/path/to/config.yml

# Test with debug and custom config
./ipmi_exporter --test --test.debug --config.file=/path/to/config.yml
```

### Debug Mode

The debug mode (`--test.debug`) provides detailed information about each test:

**What Debug Shows:**
- **Metric Name**: Clean metric name (e.g., `ipmi_bmc_info`)
- **Description**: Human-readable description of what the metric measures
- **Available Labels**: Label names available for filtering (e.g., `id`, `name`, `state`)
- **Status**: Confirmation that the metric was collected successfully

**Example Debug Output:**
```
=== DEBUG METRICS: bmc_info ===
Metric Name: ipmi_bmc_info
Description: Constant metric with value '1' providing details about the BMC.
Available Labels: {firmware_revision,manufacturer_id,system_firmware_version,bmc_url}
Status: Metric collected successfully
=== END DEBUG METRICS ===
```

**Debug Use Cases:**
- **Metric Discovery**: See what metrics are available
- **Label Inspection**: Understand metric structure
- **Troubleshooting**: Identify why metrics aren't appearing
- **Development**: Debug new collector implementations

### What Gets Tested

The test suite validates all available IPMI collectors:

#### FreeIPMI Collectors
- **bmc_info** - BMC device information
- **chassis_info** - Chassis information  
- **dcmi_info** - DCMI power management information
- **ipmi_sensor** - IPMI sensor readings
- **sel_info** - System Event Log information
- **sel_events** - System Event Log events
- **bmc_watchdog** - BMC watchdog timer information
- **sm_lan_mode** - Shared memory LAN mode information

#### Native IPMI Collectors (when `--native-ipmi` is used)
- **bmc_info_native** - BMC device information (Native)
- **chassis_info_native** - Chassis information (Native)
- **dcmi_info_native** - DCMI power management information (Native)
- **ipmi_sensor_native** - IPMI sensor readings (Native)
- **sel_info_native** - System Event Log information (Native)
- **sel_events_native** - System Event Log events (Native)
- **bmc_watchdog_native** - BMC watchdog timer information (Native)
- **sm_lan_mode_native** - Shared memory LAN mode information (Native)

### Test Results

The test suite provides:

#### Real-time Logging
- Individual test execution status
- Detailed error messages for failures
- Performance metrics (duration, metrics count)

#### Results Table
```
========================================================================================================================
TEST NAME                 DESCRIPTION                         STATUS    DURATION     METRICS   ERROR
------------------------------------------------------------------------------------------------------------------------
bmc_info                  Get BMC device information          PASS      1.234s       15        
chassis_info              Get chassis information             PASS      0.987s       8         
ipmi_sensor               Get IPMI sensor readings           FAIL      2.145s       0         permission denied
sel_info                  Get System Event Log information    PASS      0.456s       23        
------------------------------------------------------------------------------------------------------------------------
SUMMARY: 3 PASSED, 1 FAILED, 4 TOTAL
TOTAL DURATION: 4.822s
IMPLEMENTATION: FreeIPMI
========================================================================================================================
```

#### Failed Test Traces
For failed tests, detailed traces are shown:
```
=== FAILED TEST TRACE: ipmi_sensor ===
Command: ipmi-sensor
Args: [--quiet-cache, --output-sensor-state, --comma-separated-output]
Output length: 0 bytes
Metrics collected: 0
=== END TRACE ===
```

### Test Criteria

A test is considered **PASSED** when:
1. ✅ Command executes without error
2. ✅ At least one metric is collected
3. ✅ Output data is generated

A test is considered **FAILED** when:
1. ❌ Command execution fails (permission, hardware issues)
2. ❌ No metrics are collected
3. ❌ Collection process fails

### Exit Codes

- **0** - All tests passed
- **1** - One or more tests failed

### Use Cases

#### Validation After Installation
```bash
# Verify IPMI exporter works on new hardware
./ipmi_exporter --test
```

#### Comparing Implementations
```bash
# Test FreeIPMI
./ipmi_exporter --test > freeipmi_results.txt

# Test Native IPMI  
./ipmi_exporter --test --native-ipmi > native_results.txt

# Compare results
diff freeipmi_results.txt native_results.txt
```

#### Troubleshooting
```bash
# Run tests to identify failing collectors
./ipmi_exporter --test

# Check specific error traces in output
# Fix permissions/hardware issues
# Re-run tests to verify fixes
```

#### Continuous Integration
```bash
# In CI/CD pipeline
./ipmi_exporter --test
if [ $? -eq 0 ]; then
    echo "All IPMI tests passed"
else
    echo "Some IPMI tests failed"
    exit 1
fi
```

### Common Issues and Solutions

#### Permission Denied Errors
```bash
# Add user to ipmi group or run with sudo
sudo usermod -a -G ipmi $USER
# or
sudo ./ipmi_exporter --test
```

#### Hardware Not Supported
- Some IPMI features may not be available on all hardware
- This is normal - focus on the collectors that work for your hardware

#### Native IPMI Issues
```bash
# Native IPMI is experimental, may have limited support
# Fall back to FreeIPMI if issues occur
./ipmi_exporter --test  # without --native-ipmi
```

### Integration with Monitoring

The test mode can be integrated into monitoring systems:

#### Prometheus Alert
```yaml
groups:
- name: ipmi_exporter
  rules:
  - alert: IPMIExporterTestsFailing
    expr: up{job="ipmi-exporter-tests"} == 0
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "IPMI exporter tests are failing"
      description: "IPMI exporter test suite failed on {{ $labels.instance }}"
```

#### Cron Job
```bash
# Run tests daily and log results
0 2 * * * /usr/local/bin/ipmi_exporter --test >> /var/log/ipmi_tests.log 2>&1
```

### Development

When adding new IPMI collectors:

1. Add test case to `test.go`
2. Implement collector interface
3. Test with `--test` flag
4. Verify results table shows new collector

The test suite provides confidence that IPMI functionality works correctly across different hardware and implementations.
