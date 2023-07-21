# Components Overview

yawol has three main components: The `yawol-cloud-controller`, the
`yawol-controller`, and the `yawollet`.

The figure below shows the interaction of these components on a high level. Read
on for a detailed description. 

![Overview](overview.drawio.svg)

## The yawol-cloud-controller

The yawol-cloud-controller watches the Kubernetes cluster and translates
information back and forth between Kubernetes `Services`/`Nodes` and
`LoadBalancers`. `LoadBalancers` are one of the CRDs used by yawol. Whenever the
user creates a `Service` with `type: LoadBalancer`, the yawol-cloud-controller
creates a corresponding `LoadBalancer` resource.

Once the Load Balancer is ready (courtesy of yawol-controller, see below), the
yawol-cloud-controller reports the external IP back to the `Service`.
Additionally, the yawol-cloud-controller updates endpoint lists on the `Nodes`.

### Controllers

The yawol-cloud-controller is split in two controllers. This allows to save the
`LoadBalancer` objects in another cluster than the `Services` are placed.

#### **control-controller**

* Copies events from `LoadBalancer` to `Service`
* Writes external IP from `LoadBalancer` to `Service` once the LB is running and
  ready

#### **target-controller**

* `node-controller`
  * Watches K8s nodes and updates `LoadBalancer` endpoint list
* `service-controller`
  * creates a `LoadBalancer` from `Service` and enriches it with additional
	OpenStack data from environment variables

## The yawol-controller

The yawol-controller creates the needed OpenStack resources for any
`LoadBalancer` object. It manages the following OpenStack resources:

* Floating IP
* Port
* SecurityGroup
* Instance (VM)

The instance is equipped with a `cloud-init` for the following settings:

* `kubeconfig` and settings for yawollet (see below)
* Debug settings

In order to manage these resources "the Kubernetes way", yawol adopts the
`Deployment` -> `ReplicaSet` -> `Pod` cascade and does the following:

`LoadBalancer` -> `LoadBalancerSet` -> `LoadBalancerMachine`

Where the `LoadBalancer` resource holds information on Floating IP, Port and
SecurityGroup; the `LoadBalancerSet` resource recreates `LoadBalancerMachines`
whenever they get unhealthy; and the `LoadBalancerMachine` itself represents the
OpenStack instance where the yawollet and Envoy do the actual Load Balancing.

### Controllers

#### **loadbalancer-controller**

* Create/Reconcile/Delete following OpenStack resources for a `LoadBalancer`:
	* Floating IP
	* Port
	* SecurityGroup
* Creates/Recreate/Delete `LoadBalancerSet` if `LoadBalancer` is created/updated

#### **loadbalancerset-controller**

* Creates/Deletes `LoadBalancerMachines` from `LoadbalancerSet`
* Monitor `LoadBalancerMachine` status and recreates `LoadBalancerMachine` if LoadBalancer is unhealthy.

> A `LoadBalancerMachine` will me marked as unready if any of the following criteria matches:
> - no conditions are set
> - at least one LastHeartbeatTime of any condition is older than 3 min
> - condition `ConfigReady`, `EnvoyReady` or `EnvoyUpToDate` is false
> 
> This will be represented in the status and in cases of scale down unready machines will be deleted first.
> 
> If a machine takes longer than 10 min to start (if no condition is present) it will be recreated. 
> A machine which condition `ConfigReady`, `EnvoyReady` or `EnvoyUpToDate` is false for more than 5 min or 
> if the heartbeat for any condition is older than 5 min will be recreated.

#### **loadbalancermachine-controller**

* Create/Reconcile/Delete following OpenStack resources for a `LoadBalancerMachine`:
	* Instance (VM)
		* With cloud-init for the following settings
			* Kubeconfig for yawollet
			* Settings for yawollet
			* Debug settings
	* Connect instance to port
* Export metrics from `LoadBalancerMachine`

### Metrics

The yawol-controller provides some metrics which are exposed via the `/metrics` endpoint.

| metric                                 | description                                                                                                                                   | exposed by                                      |
|----------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------|
| yawol_openstack                        | Openstack usage counter by api, object, operation                                                                                             | loadbalancer and loadbalancermachine controller |
| loadbalancer_info                      | Loadbalancer Info for LoadBalancer contains labels like isInternal, externalIP, tcpProxyProtocol, tcpIdleTimeout, udpIdleTimeout, lokiEnabled | loadbalancer controller                         |
| loadbalancer_openstack_info            | Openstack Info contains labels with the OpenStackIDs for LoadBalancer                                                                         | loadbalancer controller                         |
| loadbalancer_replicas                  | Replicas for LoadBalancer (from lb.spec.replicas, 0 if marked for deletion)                                                                   | loadbalancer controller                         |
| loadbalancer_replicas_current          | Current replicas for LoadBalancer (from lb.status.replicas)                                                                                   | loadbalancer controller                         |
| loadbalancer_replicas_ready            | Ready replicas for LoadBalancer (from lb.status.readyReplicas)                                                                                | loadbalancer controller                         |
| loadbalancer_deletion_timestamp        | Deletion timestamp of a LoadBalancer in seconds since epoch (only present for LoadBalancers in deletion)                                      | loadbalancer controller                         |
| loadbalancerset_replicas               | Replicas for LoadBalancerSet (from lbs.spec.replicas, 0 if marked for deletion)                                                               | loadbalancerset controller                      |
| loadbalancerset_replicas_current       | Current replicas for LoadBalancerSet (from lbs.status.replicas)                                                                               | loadbalancerset controller                      |
| loadbalancerset_replicas_ready         | Ready replicas for LoadBalancerSet (from lbs.status.readyReplicas)                                                                            | loadbalancerset controller                      |
| loadbalancerset_deletion_timestamp     | Deletion timestamp of a LoadBalancerSet in seconds since epoch (only present for LoadBalancerSets in deletion)                                | loadbalancerset controller                      |
| loadbalancermachine                    | Metrics of loadbalancermachine (all metrics from lbm.status.metrics)                                                                          | loadbalancermachine controller                  |
| loadbalancermachine_condition          | Conditions of loadbalancermachine (lbm.status.conditions)                                                                                     | loadbalancermachine controller                  |
| loadbalancermachine_openstack_info     | Openstack Info contains labels with the OpenStackIDs for LoadBalancerMachine                                                                  | loadbalancermachine controller                  |
| loadbalancermachine_deletion_timestamp | Deletion timestamp of a LoadBalancerMachine in seconds since epoch (only present for LoadBalancerMachines in deletion)                        | loadbalancermachine controller                  |

## yawollet

The yawollet is running on an OpenStack instance (like the `kubelet`) to
configure Envoy with information from the corresponding `LoadBalancer` object in
the Kubernetes cluster. To get this information, the yawollet uses a
`kubeconfig` that is provided by the yawol-controller via `cloud-init`.

### Metrics

The yawollet exposes metrics via the `LoadBalancerMachine` Object (`.status.metrics`). 
If the yawollet cant get a metric this metric is ignored to get always as much metrics as possible 
(for example the keepalived metrics cant be parsed all metrics from keepalived will be ignored)

List of metrics:

| metric                           | description                                        |
|----------------------------------|----------------------------------------------------|
| load1                            | load1 from the vm                                  |
| load5                            | load5 from the vm                                  |
| load15                           | load15 from the vm                                 |
| numCPU                           | number of CPU Cores from the VM                    |
| memTotal                         | total memory from the vm in kB                     |
| memFree                          | free memory from the vm in kB                      |
| memAvailable                     | available memory from the vm in kB                 |
| stealTime                        | stealTime in hundredths of a second since vm start |
| keepalivedIsMaster               | keepalived master status                           |
| keepalivedBecameMaster           | keepalived counter became master                   |
| keepalivedReleasedMaster         | keepalived counter released master                 |
| keepalivedAdvertisementsSent     | keepalived counter of sent advertisements          |
| keepalivedAdvertisementsReceived | keepalived counter of received advertisements      |
| *-upstream_cx_active             | currently active connections per port              |
| *-upstream_cx_total              | total connections since start per port             |
| *-upstream_cx_rx_bytes_total     | bytes sent since start per port (lb -> client)     |
| *-upstream_cx_tx_bytes_total     | bytes sent since start per port (client -> lb)     |
| envoytcp-idle_timeout            | total idle tcp connections since start             |
