package helper

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	yawolv1beta1 "github.com/stackitcloud/yawol/api/v1beta1"

	"github.com/prometheus/client_golang/prometheus"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerOpenstackReconcileIsNeeded returns true if an openstack reconcile is needed.
func LoadBalancerMachineOpenstackReconcileIsNeeded(lbm *yawolv1beta1.LoadBalancerMachine) bool {
	// LastOpenstackReconcile is nil, first run
	if lbm.Status.LastOpenstackReconcile == nil {
		return true
	}

	// lastOpenstackReconcile is older than 5 min
	if lbm.Status.LastOpenstackReconcile.Before(&metaV1.Time{Time: time.Now().Add(-5 * time.Minute)}) {
		return true
	}

	return false
}

func GetHashForLoadBalancerMachineSpecFromLoadBalancer(lb *yawolv1beta1.LoadBalancer) (string, error) {
	var floatingID string
	if lb.Status.FloatingID != nil {
		floatingID = *lb.Status.FloatingID
	}

	return HashData(yawolv1beta1.LoadBalancerMachineSpec{
		Infrastructure: lb.Spec.Infrastructure,
		FloatingID:     floatingID,
		LoadBalancerRef: yawolv1beta1.LoadBalancerRef{
			Namespace: lb.Namespace,
			Name:      lb.Name,
		},
	})
}

func ParseLoadBalancerMachineMetrics(p prometheus.GaugeVec, loadBalancerMachine *yawolv1beta1.LoadBalancerMachine) {
	if loadBalancerMachine.Status.Metrics == nil {
		return
	}
	for _, metric := range *loadBalancerMachine.Status.Metrics {
		if value, err := strconv.ParseFloat(metric.Value, 64); err == nil {
			// metric labels: type, lb, lbm, namespace
			p.WithLabelValues(metric.Type,
				loadBalancerMachine.Spec.LoadBalancerRef.Name,
				loadBalancerMachine.Name,
				loadBalancerMachine.Namespace).Set(value)
		}
	}
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
	loadBalancerName string,
	loadBalancerMachineName string,
	namespace string,
	debug bool,
	vip string,
) string {
	bk := base64.StdEncoding.EncodeToString([]byte(kubeconfig))
	keepalivedConfig := base64.StdEncoding.EncodeToString(
		[]byte(generateKeepalivedConfig(vip)),
	)

	var systemctlSshd, openrcSshd, openrcState string
	if debug {
		systemctlSshd = "enable"
		openrcSshd = "add"
		openrcState = "start"
	} else {
		systemctlSshd = "disable"
		openrcSshd = "del"
		openrcState = "stop"
	}

	tpl := `
#cloud-config
write_files:
- encoding: b64
  content: ` + bk + `
  owner: yawol:yawol
  path: /etc/yawol/kubeconfig
  permissions: '0600'
- encoding: b64
  content: ` + keepalivedConfig + `
  owner: root:root
  path: /etc/keepalived/keepalived.conf
  permissions: '0644'
- content: >
    YAWOLLET_ARGS="-namespace=` + namespace + `
    -loadbalancer-name=` + loadBalancerName + `
    -loadbalancer-machine-name=` + loadBalancerMachineName + `
    -listen-address=` + vip + `
    -kubeconfig /etc/yawol/kubeconfig"
  path: /etc/yawol/env.conf
runcmd:
  - [ /sbin/rc-service, sshd, ` + openrcState + ` ]
  - [ /sbin/rc-update, ` + openrcSshd + `, sshd, default ]
  - [ systemctl, ` + systemctlSshd + `, ssh.service, --now ]
  - [ systemctl, ` + systemctlSshd + `, sshd.service, --now ]
  - [ systemctl, daemon-reload ]
  - [ systemctl, restart, keepalived.service ]
  - [ systemctl, restart, yawollet.service ]
  - [ systemctl, restart, envoy.service ]
  - [ /sbin/rc-service, keepalived, restart ]
  - [ /sbin/rc-service, envoy, restart ]
  - [ /sbin/rc-service, yawollet, restart ]
`
	return tpl
}

func generateKeepalivedConfig(vip string) string {
	return `
! Configuration File for keepalived

global_defs {
	router_id envoy
	max_auto_priority -1
}

vrrp_track_process envoy {
	process envoy
	weight 100
}

vrrp_instance ` + VRRPInstanceName + ` {
	state MASTER
	interface eth0
	virtual_router_id 100
	priority 100
	advert_int 1

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
}
	`
}
