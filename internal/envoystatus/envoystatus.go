// Package envoystatus is used in yawollet to get current envoy status
package envoystatus

import (
	"io"
	"net/http"
	"strings"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Config is to configure envoystatus functions
type Config struct {
	// AdminAddress is the address for admin interface of envoy without http:// and without / at the end
	// Example: localhost:9000
	AdminAddress string
}

// GetEnvoyStatus returns true if envoy ready endpoint returns 200 otherwise return false
func (c *Config) GetEnvoyStatus() bool {
	resp, _ := http.Get("http://" + c.AdminAddress + "/ready")
	if resp != nil && resp.StatusCode == 200 {
		_ = resp.Body.Close()
		return true
	}
	return false
}

// GetCurrentSnapshotVersion returns current snapshot version from envoy
func (c *Config) GetCurrentSnapshotVersion() (
	clusterVersion string,
	listenerVersion string,
	err error,
) {
	resp, err := http.Get("http://" + c.AdminAddress + "/stats?filter=cluster_manager.cds.version_text|listener_manager.lds.version_text")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close() //nolint:errcheck // don't use error in defer

	// check if not 200 and return an empty snapshot version if status code is not 200
	if resp.StatusCode != 200 {
		return "", "", nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	// example output from request:
	// cluster_manager.cds.version_text: "3"
	// should normally just one line if the filter works, but iterate over lines if there is more than one
	for _, stats := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(stats, "cluster_manager.cds.version_text: ") {
			clusterVersion = strings.TrimPrefix(stats, "cluster_manager.cds.version_text: ")
			clusterVersion = strings.Trim(clusterVersion, "\"")
		}

		if strings.HasPrefix(stats, "listener_manager.lds.version_text: ") {
			listenerVersion = strings.TrimPrefix(stats, "listener_manager.lds.version_text: ")
			listenerVersion = strings.Trim(listenerVersion, "\"")
		}
	}

	return clusterVersion, listenerVersion, nil
}

// GetCurrentStats returns a view stats for envoy
func (c *Config) GetCurrentStats() ([]yawolv1beta1.LoadBalancerMachineMetric, error) {
	resp, err := http.Get("http://" + c.AdminAddress + "/stats?filter=cluster|tcp")
	if err != nil {
		return []yawolv1beta1.LoadBalancerMachineMetric{}, err
	}
	defer resp.Body.Close() //nolint:errcheck // don't use error in defer

	// check if not 200 and return an empty snapshot version if status code is not 200
	if resp.StatusCode != 200 {
		return []yawolv1beta1.LoadBalancerMachineMetric{}, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []yawolv1beta1.LoadBalancerMachineMetric{}, err
	}

	return parseEnvoyMetricsToLoadBalancerMachineMetrics(string(body)), nil
}

// parseEnvoyMetricsToLoadBalancerMachineMetrics returns a LoadBalancerMachineMetric for Envoy
func parseEnvoyMetricsToLoadBalancerMachineMetrics(envoyMetrics string) []yawolv1beta1.LoadBalancerMachineMetric {
	var metrics []yawolv1beta1.LoadBalancerMachineMetric
	for _, stats := range strings.Split(envoyMetrics, "\n") {
		if strings.HasPrefix(stats, "cluster") {
			if strings.Contains(stats, "upstream_cx_active") ||
				strings.Contains(stats, "upstream_cx_total") ||
				strings.Contains(stats, "upstream_cx_connect_fail") ||
				strings.Contains(stats, "upstream_cx_tx_bytes_total") ||
				strings.Contains(stats, "upstream_cx_rx_bytes_total") {
				stat := strings.Split(stats, ".")
				if len(stat) != 3 {
					continue
				}
				if strings.Contains(stat[1], "TCP") || strings.Contains(stat[1], "UDP") {
					metrics = append(metrics, yawolv1beta1.LoadBalancerMachineMetric{
						Type:  stat[1] + "-" + strings.Split(stat[2], ":")[0],
						Value: strings.TrimPrefix(stat[2], strings.Split(stat[2], ":")[0]+": "),
						Time:  v1.Now(),
					})
				}
			}
		}
		if strings.HasPrefix(stats, "tcp") {
			if strings.Contains(stats, "idle_timeout") {
				stat := strings.Split(stats, ".")
				if len(stat) != 3 {
					continue
				}
				metrics = append(metrics, yawolv1beta1.LoadBalancerMachineMetric{
					Type:  stat[1] + "-idle_timeout",
					Value: strings.TrimPrefix(stat[2], strings.Split(stat[2], ":")[0]+": "),
					Time:  v1.Now(),
				})
			}
		}
	}

	return metrics
}
