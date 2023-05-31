package helper

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"
	helpermetrics "github.com/stackitcloud/yawol/internal/metrics"

	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerMachineOpenstackReconcileIsNeeded returns true if an openstack reconcile is needed.
func LoadBalancerMachineOpenstackReconcileIsNeeded(lbm *yawolv1beta1.LoadBalancerMachine) bool {
	// LastOpenstackReconcile is nil, first run
	if lbm.Status.LastOpenstackReconcile == nil {
		return true
	}

	// lastOpenstackReconcile is older than 5 min
	// add some seconds in order to be sure it reconciles
	if lbm.Status.LastOpenstackReconcile.Before(&metaV1.Time{Time: time.Now().Add(-OpenstackReconcileTime).Add(2 * time.Second)}) {
		return true
	}

	return false
}

func GetHashForLoadBalancerMachineSet(lb *yawolv1beta1.LoadBalancer) (string, error) {
	var portID string
	if lb.Status.PortID != nil {
		portID = *lb.Status.PortID
	}

	var serverGroupName string
	if lb.Status.ServerGroupName != nil {
		serverGroupName = *lb.Status.ServerGroupName
	}

	return HashData(map[string]interface{}{
		"loadBalancerMachineSpec": yawolv1beta1.LoadBalancerMachineSpec{
			Infrastructure: lb.Spec.Infrastructure,
			PortID:         portID,
			ServerGroupID:  serverGroupName,
			LoadBalancerRef: yawolv1beta1.LoadBalancerRef{
				Namespace: lb.Namespace,
				Name:      lb.Name,
			},
		},
		"logForward":        lb.Spec.Options.LogForward,
		"debugSettings":     lb.Spec.DebugSettings,
		"serverGroupPolicy": lb.Spec.Options.ServerGroupPolicy,
	})
}

func ParseLoadBalancerMachineMetrics(
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	metrics *helpermetrics.LoadBalancerMachineMetricList,
) {
	parseLoadBalancerMachineVMMetrics(loadBalancerMachine, metrics)
	parseLoadBalancerMachineConditionsMetrics(loadBalancerMachine, metrics)
	parseLoadBalancerMachineOpenstackInfoMetrics(loadBalancerMachine, metrics)
}

func parseLoadBalancerMachineVMMetrics(
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	metrics *helpermetrics.LoadBalancerMachineMetricList,
) {
	if loadBalancerMachine.Status.Metrics == nil || metrics == nil || metrics.VM == nil {
		return
	}
	for _, metric := range *loadBalancerMachine.Status.Metrics {
		if value, err := strconv.ParseFloat(metric.Value, 64); err == nil {
			// metric labels: type, lb, lbm, namespace
			metrics.VM.WithLabelValues(metric.Type,
				loadBalancerMachine.Spec.LoadBalancerRef.Name,
				loadBalancerMachine.Name,
				loadBalancerMachine.Namespace).Set(value)
		}
	}
}

func parseLoadBalancerMachineConditionsMetrics(
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	metrics *helpermetrics.LoadBalancerMachineMetricList,
) {
	if loadBalancerMachine.Status.Conditions == nil || metrics == nil || metrics.Conditions == nil {
		return
	}
	for _, conditions := range *loadBalancerMachine.Status.Conditions {
		metrics.Conditions.DeletePartialMatch(map[string]string{
			"lb":        loadBalancerMachine.Spec.LoadBalancerRef.Name,
			"lbm":       loadBalancerMachine.Name,
			"namespace": loadBalancerMachine.Namespace,
			"condition": string(conditions.Type),
		})
		// metric labels: lb, lbm, namespace, condition, reason, status
		metrics.Conditions.WithLabelValues(
			loadBalancerMachine.Spec.LoadBalancerRef.Name,
			loadBalancerMachine.Name,
			loadBalancerMachine.Namespace,
			string(conditions.Type),
			conditions.Reason,
			string(conditions.Status),
		).Set(1)
	}
}

func parseLoadBalancerMachineOpenstackInfoMetrics(
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	metrics *helpermetrics.LoadBalancerMachineMetricList,
) {
	if metrics == nil || metrics.OpenstackInfoMetrics == nil {
		return
	}
	labels := map[string]string{
		"lb":        loadBalancerMachine.Spec.LoadBalancerRef.Name,
		"lbm":       loadBalancerMachine.Name,
		"namespace": loadBalancerMachine.Namespace,
		"portID":    "nil",
		"serverID":  "nil",
	}

	if loadBalancerMachine.Status.DefaultPortID != nil {
		labels["portID"] = *loadBalancerMachine.Status.DefaultPortID
	}

	if loadBalancerMachine.Status.ServerID != nil {
		labels["serverID"] = *loadBalancerMachine.Status.ServerID
	}

	metrics.OpenstackInfoMetrics.DeletePartialMatch(map[string]string{
		"lb":        loadBalancerMachine.Spec.LoadBalancerRef.Name,
		"lbm":       loadBalancerMachine.Name,
		"namespace": loadBalancerMachine.Namespace,
	})
	metrics.OpenstackInfoMetrics.With(labels).Set(1)
}

func RemoveLoadBalancerMachineMetrics(
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
	metrics *helpermetrics.LoadBalancerMachineMetricList,
) {
	if metrics == nil ||
		metrics.VM == nil {
		return
	}

	labels := map[string]string{
		"lb":        loadBalancerMachine.Spec.LoadBalancerRef.Name,
		"lbm":       loadBalancerMachine.Name,
		"namespace": loadBalancerMachine.Namespace,
	}

	metrics.VM.DeletePartialMatch(labels)
	metrics.Conditions.DeletePartialMatch(labels)
	metrics.OpenstackInfoMetrics.DeletePartialMatch(labels)
}

// RemoveFromLBMStatus removes key from loadbalancermachine status.
func RemoveFromLBMStatus(ctx context.Context, sw client.StatusWriter, lbm *yawolv1beta1.LoadBalancerMachine, key string) error {
	patch := []byte(`{"status":{"` + key + `": null}}`)
	return sw.Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch))
}

func GetImageID(spec yawolv1beta1.OpenstackImageRef) (string, error) {
	if spec.ImageID != nil {
		return *spec.ImageID, nil
	}
	if spec.ImageName != nil {
		// TODO get imageid from name
		return "", nil
	}
	if spec.ImageSearch != nil {
		// TODO get imageid by metadata search
		return "", nil
	}
	return "", fmt.Errorf("imageid not found")
}

func GetFlavorID(spec yawolv1beta1.OpenstackFlavorRef) (string, error) {
	if spec.FlavorID != nil {
		return *spec.FlavorID, nil
	}
	if spec.FlavorName != nil {
		// TODO get flavorid from name
		return "", nil
	}
	if spec.FlavorSearch != nil {
		// TODO get flavorid by metadata search
		return "", nil
	}
	return "", fmt.Errorf("flavorid not found")
}

// PatchLBMStatus patch loadbalancermachine status
func PatchLBMStatus(
	ctx context.Context,
	sw client.StatusWriter,
	lbm *yawolv1beta1.LoadBalancerMachine,
	lbmStatus yawolv1beta1.LoadBalancerMachineStatus,
) error {
	lbStatusJSON, err := json.Marshal(lbmStatus)
	if err != nil {
		return err
	}
	patch := []byte(`{"status":` + string(lbStatusJSON) + `}`)
	return sw.Patch(ctx, lbm, client.RawPatch(types.MergePatchType, patch))
}

func GenerateUserData(
	kubeconfig string,
	loadbalancer *yawolv1beta1.LoadBalancer,
	loadbalancerMachine *yawolv1beta1.LoadBalancerMachine,
	vip string,
	yawolletRequeueTime int,
) string {
	const (
		openRCDel   = "del"
		openRCAdd   = "add"
		openRCStart = "start"
		openRCStop  = "stop"
	)

	kubeconfigBase64 := base64.StdEncoding.EncodeToString([]byte(kubeconfig))
	keepalivedConfigBase64 := base64.StdEncoding.EncodeToString([]byte(generateKeepalivedConfig(vip)))

	promtailConfig := "disabled"
	promtailOpenRC := openRCDel
	promtailOpenRCState := openRCStop
	if loadbalancer.Spec.Options.LogForward.Enabled {
		promtailConfig = generatePromtailConfig(loadbalancer, loadbalancerMachine)
		promtailOpenRC = openRCAdd
		promtailOpenRCState = openRCStart
	}
	promtailConfigBase64 := base64.StdEncoding.EncodeToString([]byte(promtailConfig))

	sshOpenRC := openRCDel
	sshOpenRCState := openRCStop
	if loadbalancer.Spec.DebugSettings.Enabled {
		sshOpenRC = openRCAdd
		sshOpenRCState = openRCStart
	}

	var yawolletArgs string
	yawolletArgs = yawolletArgs + "-namespace=" + loadbalancerMachine.Namespace + " "
	yawolletArgs = yawolletArgs + "-loadbalancer-name=" + loadbalancer.Name + " "
	yawolletArgs = yawolletArgs + "-loadbalancer-machine-name=" + loadbalancerMachine.Name + " "
	yawolletArgs = yawolletArgs + "-listen-address=" + vip + " "
	yawolletArgs = yawolletArgs + "-kubeconfig /etc/yawol/kubeconfig" + " "

	if yawolletRequeueTime > 0 {
		yawolletArgs = yawolletArgs + "-requeue-time=" + strconv.Itoa(yawolletRequeueTime) + " "
	}

	return `
#cloud-config
write_files:
- encoding: b64
  content: ` + kubeconfigBase64 + `
  owner: yawol:yawol
  path: /etc/yawol/kubeconfig
  permissions: '0600'
- encoding: b64
  content: ` + keepalivedConfigBase64 + `
  owner: root:root
  path: /etc/keepalived/keepalived.conf
  permissions: '0644'
- encoding: b64
  content: ` + promtailConfigBase64 + `
  owner: promtail:promtail
  path: /etc/promtail/promtail.yaml
  permissions: '0644'
- content: >
    YAWOLLET_ARGS="` + yawolletArgs + `"
  path: /etc/yawol/env.conf
runcmd:
  - [ /sbin/rc-service, promtail, ` + promtailOpenRCState + ` ]
  - [ /sbin/rc-update, ` + promtailOpenRC + `, promtail, default ]
  - [ /sbin/rc-service, sshd, ` + sshOpenRCState + ` ]
  - [ /sbin/rc-update, ` + sshOpenRC + `, sshd, default ]
  - [ /sbin/rc-service, keepalived, restart ]
  - [ /sbin/rc-service, envoy, restart ]
  - [ /sbin/rc-service, yawollet, restart ]
`
}

func generateKeepalivedConfig(vip string) string {
	return `! Configuration File for keepalived

global_defs {
	router_id envoy
	max_auto_priority -1
	vrrp_garp_master_repeat 5
	vrrp_garp_interval 1
	vrrp_garp_lower_prio_repeat 1
	vrrp_garp_master_refresh 300
	vrrp_garp_master_refresh_repeat 1
	vrrp_higher_prio_send_advert true
}

vrrp_track_process envoy {
	process envoy
	weight 100
}

vrrp_instance ` + VRRPInstanceName + ` {
	state BACKUP
	interface eth0
	nopreempt
	virtual_router_id 100
	priority 50
	advert_int 4

	authentication {
		auth_type PASS
		auth_pass yawol
	}

	virtual_ipaddress {
		` + vip + `
	}

	track_process {
		envoy
	}
}`
}

func generatePromtailConfig(
	loadBalancer *yawolv1beta1.LoadBalancer,
	loadBalancerMachine *yawolv1beta1.LoadBalancerMachine,
) string {
	return `server:
  disable: true

positions:
  filename: /tmp/positions.yaml

clients:
  - url: '` + loadBalancer.Spec.Options.LogForward.LokiURL + `'

scrape_configs:
  - job_name: messages
    static_configs:
      - targets:
          - localhost
        labels:
          job: yawol-logs
          lbm: ` + loadBalancerMachine.Name + `
          lb: ` + loadBalancer.Name + `
          application: messages
          __path__: /var/log/messages`
}
