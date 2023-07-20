# Development

## Earthly

We use [Earthly](https://github.com/earthly/earthly) instead of a `Makefile`

## Run tests ans lint

### go tests
```bash
earthly +test
```

### lint
```bash
earthly +lint
```

## Dev-Setup (controllers running locally)

> In the following instruction the yawollet is running within an OpenStack VM
> that's booted from an OpenStack yawollet image.

If you want to run/test the yawollet locally see [local-yawollet](#local-yawollet)

### Requirements

If you only want the `yawol-cloud-controller` (To test creation of
`LoadBalancer` from `Service`):

* Any kind of Kubernetes cluster (remote or local with `kind`)

If you want to develop end-to-end (`yawol-cloud-controller` and
`yawol-controller` locally, `yawollet` on VM):

* Access to a K8s cluster that is publicly reachable
* Access to OpenStack project via OpenStack API

### Preparation

1. Generate and install yawol CRDs

   ```bash
   earthly +generate
   kubectl apply -f charts/yawol-controller/crds/
   ```
2. Edit environment variables in `run-ycc.sh`

   These variables are required for yawol-cloud-controller and are later used by
   yawol-controller. For a local cluster the variables can be left as is, for a
   remote cluster set the variables to match the OpenStack resources:

   * `FLOATING_NET_ID`: ID of `floating-net`
   * `NETWORK_ID`: ID of the network
   * To use a different yawollet OpenStack image set `IMAGE_ID`. If testing in a
     different OpenStack project, make sure that the image can be accessed by
     the project. Set `visibility` to not be `private`, e.g.

     ```bash
     openstack image set --shared <ID>
     openstack image add project <image> <project>
     ```

3. Edit environment variables in `run-yc.sh`

   These variables are required for yawol-controller:
  
   * `API_ENDPOINT` = `https://` + IP/URL for Kubernetes API server (used by
     yawollet)

4. Create `cloud-provider-config` secret (required for yawol-controller and
   later used by yawollet)

   Use `example-setup/yawol-controller/provider-config.yaml` as template. The
   namespace needs to match `CLUSTER_NAMESPACE` in `run-ycc.sh` and `run-yc.sh`

### Run

The controllers are using the default kubeconfig ($KUBECONFIG, InCluster or
\$HOME/.kube/config). To use a different kubeconfig see the instructions below. 

1. Run `yawol-cloud-controller`. To use a different kubeconfig set the
   `--control-kubeconfig` and`--target-kubeconfig` flags in `run-ycc.sh`.

   ```bash
   ./run-ycc.sh
   ```

2. Run `yawol-controller`. To use a different kubeconfig set the `--kubeconfig`
   flag in `./run-yc.sh`.

   ```bash
   ./run-yc.sh
   ```

### Verify

**yawol-cloud-controller**

1. Create deployment and service:

   ```bash
   kubectl apply -f example-setup/yawol-cloud-controller
   # or
   kubectl create deployment --image nginx:latest nginx --replicas 1
   kubectl expose deployment --port 80 --type LoadBalancer nginx --name loadbalancer
   ```

2. Check if the yawol-cloud-controller created a new `LoadBalancer` object

**yawol-controller**

1. Reuse created `LoadBalancer` from yawol-cloud-controller **or** create a new
   one (use `example-setup/yawol-controller/loadbalancer.yaml` as template)

3. Check if the yawol-controller (loadbalancer-controller) created OpenStack resources (FloatingIP, Port, SecurityGroup) for the `LoadBalancer`
3. Check if the yawol-controller (loadbalancer-controller) created a `LoadbalancerSet` from the `LoadBalancer`
4. Check if the yawol-controller (loadbalancerset-controller) created a `LoadbalancerMachines` from the `LoadbalancerSet`
5. Check if the yawol-controller (loadbalancermachine-controller) created and configured an OpenStack VM for the `LoadbalancerMachine`
6. Once the VM (`LBM`) is ready check if the yawol-cloud-controller wrote the IP to the `Service`

## Local yawollet

### Requirements

* Any kind of Kubernetes cluster (remote or local with `kind`)
* Envoy locally installed `earthly +get-envoy-local` (downloaded from envoy docker image)

### Preparation

1. Generate and install yawol CRDs:

   ```bash
   earthly +generate
   kubectl apply -f charts/yawol-controller/crds/
   ```

2. Create `LoadBalancer` and `LoadBalancerMachine` object (use examples in
   `example-setup/yawollet/`):

   ```
   kubectl apply -f example-setup/yawollet/lb.yaml
   kubectl apply -f example-setup/yawollet/lbm.yaml
   ```

   This example adds an TCP LoadBalancer to forward port 8085 to localhost:9000
   which is the Envoy admin port.

### Run

1. Start Envoy:

   ```bash
   envoy -c image/envoy-config.yaml
   ```

2. Run yawollet. To use a different kubeconfig set the `--kubeconfig` flag.

   ```bash
   go run ./cmd/yawollet/main.go --namespace=yawol-test \
      --loadbalancer-name=loadbalancer-sample \
      --loadbalancer-machine-name=loadbalancermachine-sample
   ```
### Test

UDP testing with netcat:

1. `netcat -u -l -p 9001`
2. Open a new terminal
3. `netcat -u 127.0.0.1 8086`
4. Type something, hit enter and check if the message gets displayed in the first terminal

TCP testing using the admin port of Envoy:

1. Open http://localhost:8085 in your browser
2. You should get forwarded to the admin port of Envoy which is listening to localhost:9000

## Troubleshooting - SSH access to yawol VM

There are currently 2 debug options to access the `LoadBalancerMachine` VM via SSH:

### Debug settings within the `LoadBalancer` `.spec.debugSettings`
This will add the SSH key via OpenStack KeyPair. A change will recreate the `LoadBalancerMachines`, because OpenStack
KeyPairs are only possible while VM creation.

1. Upload ssh key-pair to OpenStack

```bash
openstack keypair create <name> # create new keypair
# or
openstack keypair create --public-key <path> <name> # add existing pubkey
```


2. Add the following to `LoadBalancer`:

   ```yaml
   ...
   spec:
     debugSettings:
       enabled: true
       sshkeyName: <name>
   ...
   ```

This can be also enabled with the service annotations: `yawol.stackit.cloud/debug` and `yawol.stackit.cloud/debugsshkey`

> You can login with the user: `alpine`

### Ad hoc debugging
To troubleshoot a running `LoadBalancerMachine` we added a function into the `yawollet` to be able to add a SSH key
and enable/start sshd on the fly.

This can only be enabled with annotations on the `LoadBalancer`: `yawol.stackit.cloud/adHocDebug` and `yawol.stackit.cloud/adHocDebugSSHKey`

This will not recreate the `LoadBalancerMachine`. Be aware that the `yawol.stackit.cloud/adHocDebugSSHKey` has to contain the complete
SSH public key.

> You can login with the user: `yawoldebug`

> After you are done please remove the VMs, because yawol will **not** disable SSH again.


## Image Build

For the image build ansible is used. To develop on ansible you can run in locally.
Therefore, you need to get/build all needed binaries and change to the `image` directory:

```
earthly +get-envoy-local
earthly +get-envoy-libs-local
earthly +get-promtail-local
earthly +build-local
```

Now you can run ansible:
```
ansible-playbook -i <IP-Address>, --private-key=~/.ssh/ske-key --user alpine install-alpine.yaml
```
