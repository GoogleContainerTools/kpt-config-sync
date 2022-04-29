// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"go.opencensus.io/tag"
)

// ParseMetrics connects to the local port where metrics are being forwarded to
// and parses the output into a map of metrics and measurements.
func ParseMetrics(port int) (ConfigSyncMetrics, error) {
	var out []byte
	out, err := exec.Command("curl", "-s", fmt.Sprintf("localhost:%d/metrics", port)).CombinedOutput()
	if err != nil {
		return nil, errors.Errorf("error parsing metrics from port %d: %v", port, err)
	}

	entry := strings.Split(string(out), "\n")
	csm := make(ConfigSyncMetrics)
	for _, m := range entry {
		// Time-series metrics have bucket, count, and sum measurements. We just want
		// to save one of those measurements, so we ignore `bucket` and `count`. All
		// other metric types will only have a single measurement.
		if strings.HasPrefix(m, "config_sync_") &&
			!strings.Contains(m, "_bucket") &&
			!strings.Contains(m, "_count") {
			name, err := parseMetricName(m)
			if err != nil {
				return nil, err
			}
			csm[name] = append(csm[name], Measurement{
				Tags:  parseTags(m),
				Value: parseValue(m),
			})
		}
	}
	return csm, nil
}

// parseMetricName filters for the name of the metric.

// Example input string:
//   `config_sync_api_duration_seconds_sum{root_reconciler="root-reconciler",operation="create",status="success",type="Namespace"} 0.02125483`
// Output:
//   `api_duration_seconds`
func parseMetricName(m string) (string, error) {
	regex := regexp.MustCompile(`config_sync_(.*?)(?:_sum)?[{ ]`)
	ss := regex.FindStringSubmatch(m)
	if ss != nil {
		return ss[1], nil
	}
	return "", errors.Errorf("failed to parse metric name from %v", m)
}

// parseTags filters for the metric tag keys and values.
func parseTags(m string) []tag.Tag {
	var tags []tag.Tag
	// match all tags: {operation="create",status="success",type="Namespace"}
	regex := regexp.MustCompile(`{(.*?)}`)
	ss := regex.FindStringSubmatch(m)
	if ss != nil {
		tagList := strings.Split(ss[1], ",")
		for _, t := range tagList {
			// capture key and value: <key>="<value>"
			regex = regexp.MustCompile(`(.*?)="(.*?)"`)
			ss = regex.FindStringSubmatch(t)
			if ss != nil {
				key, _ := tag.NewKey(ss[1])
				tags = append(tags, tag.Tag{
					Key:   key,
					Value: ss[2],
				})
			}
		}
	}
	return tags
}

// parseValue filters for the recorded value.
func parseValue(m string) string {
	return m[strings.LastIndex(m, " ")+1:]
}
