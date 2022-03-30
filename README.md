# yawol - yet another working openstack loadbalancer

yawol ia a loadbalancer solution for openstack, based on the kubernetes controller pattern.
yawol uses kubebuilder as k8s controller framework and gophercloud for the openstack integration

## run tests

### requirements

* install kubebuilder **v3**
* have `envoy` binary in `PATH`
    * `make get-envoy` installs `envoy` on linux and macOS

### run

```bash
make test
```
example output:
```
make test
bin/controller-gen "crd:trivialVersions=true" rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config="charts/yawol-controller/crds"
?       github.com/stackitcloud/yawol/api/v1beta1       [no test files]
?       github.com/stackitcloud/yawol/cmd/yawol-cloud-controller        [no test files]
?       github.com/stackitcloud/yawol/cmd/yawol-controller      [no test files]
?       github.com/stackitcloud/yawol/cmd/yawollet      [no test files]
ok      github.com/stackitcloud/yawol/controllers/yawol-cloud-controller/control_controller     16.071s
ok      github.com/stackitcloud/yawol/controllers/yawol-cloud-controller/target_controller      44.402s
ok      github.com/stackitcloud/yawol/controllers/yawol-controller/loadbalancer 44.394s
ok      github.com/stackitcloud/yawol/controllers/yawol-controller/loadbalancermachine  23.377s
ok      github.com/stackitcloud/yawol/controllers/yawol-controller/loadbalancerset      17.684s
ok      github.com/stackitcloud/yawol/controllers/yawollet      27.119s
?       github.com/stackitcloud/yawol/internal/envoystatus      [no test files]
?       github.com/stackitcloud/yawol/internal/hostmetrics      [no test files]
?       github.com/stackitcloud/yawol/internal/openstack        [no test files]
?       github.com/stackitcloud/yawol/internal/openstack/fake   [no test files]
?       github.com/stackitcloud/yawol/internal/openstack/testing        [no test files]
```

## yawol-cloud-controller

yawol-cloud-controller is a part of the yawol loadbalancer solution. yawol-cloud-controller translate information from a kubernetes `Service` and kubernetes `Node` into a `LoadBalancer`.

### control-controller

* copies events from `LoadBalancer` to `Service`
* writes external IP from `LoadBalancer` to `Service` if LB is up and ready

### target-controller

* node-controller
	* watches k8s nodes and updates `LoadBalancer` endpoint list
* service-controller
	* creates a `LoadBalancer` from `Service` and enriches it with additional openstack data from environment variables
	
### (local) dev-setup

#### requirements

- any kind of kubernetes cluster (local or remote)
  -  for example: create kind cluster `kind create cluster`

#### preparation

1. generate and install yawol crds
```bash
make install
```
2. export needed environment variables (these variables are required and used in yawol-controller, in the local dev setup we can set these variables to some random values)
```bash
export CLUSTER_NAMESPACE="default"
export SECRET_NAME="test"
export FLOATING_NET_ID="test"
export NETWORK_ID="test"
export FLAVOR_ID="test"
export IMAGE_ID="test"
export INTERNAL_LB=1
```

#### run

1. run yawol-cloud-controller *(yawol-cloud-controller is use the default kubeconfig if you want a different kubeconfig you can set it with the `--control-kubeconfig` and `--target-kubeconfig` flag)*
```bash
make run
#or
go run ./cmd/yawol-cloud-controller/main.go --target-kubeconfig=inClusterConfig --control-kubeconfig=inClusterConfig
```

2. create a deployment and service with kubectl, the controller should create a new lb object
```bash
kubectl create deployment --image nginx:latest nginx1 --replicas 1
kubectl expose deployment --port 80 --type LoadBalancer nginx1 --name loadbalancer`

# or
kubectl apply -f example-setup/yawol-cloud-controller
```


## yawol-controller

yawol-controller is a part of the yawol loadbalancer solution. yawol-controller creates an openstack instance (incl. other needed resources) for a `LoadBalancer` object.

### Controllers

#### loadbalancer-controller

* create/reconcile/delete following openstack resources for a `LoadBalancer`:
	* floating IP
	* port
	* securitygroup
* creates/recreate/delete `LoadBalancerSet` if `LoadBalancer` is created/updated

#### loadbalancerset-controller

* creates/deletes `LoadBalancerMachines` from `LoadbalancerSet`
* monitor `LoadBalancerMachine` status and recreates `LoadBalancerMachine` if node is not healthy

#### loadbalancermachine-controller

* create/reconcile/delete following openstack resources for a `LoadBalancerMachine`:
	* instance (vm)
		* with cloud-init for the following settings
			* kubeconfig for yawollet
			* settings for yawollet
			* debug settings
	* connect instance to port
* export metrics from `LoadBalancerMachine`

### resources

`LoadBalancer` -> `LoadBalancerSet` -> `LoadBalancerMachine`

### (local) dev-setup

#### requirements

* access to a k8s cluster that is public reachable
* access to openstack project via openstack api

#### preparation

1. generate and install yawol crds
```bash
make install
```

2. create an openstack secret in the namespace of the controller
```yaml
apiVersion: v1
stringData:
  cloudprovider.conf: |-
   [Global]
   auth-url="AUTH-URL"
   domain-name="DOMAIN-NAME"
   tenant-name="TANENT-NAME"
   username="USERNAME"
   password="PASSWORD
   region="RegionOne"
kind: Secret
metadata:
  name: cloud-provider-config
  namespace: default
type: Opaque

```
3. create an `LoadBalancer` object
```yaml
apiVersion: yawol.stackit.cloud/v1beta1
kind: LoadBalancer
metadata:
  name: lb1
  namespace: default
spec:
  replicas: 1
  endpoints:
    - name: node1
      addresses:
        - 127.0.0.1
  ports:
    - name: http
      protocol: TCP
      port: 80
      nodePort: 9000
  infrastructure:
    authSecretRef:
      name: cloud-provider-config
      namespace: default
    flavor:
      flavor_id: FLAVOR-ID
    floatingNetID: FLOATINGNET-ID
    image:
      image_id: IMAGE-ID-FOR-YAWOLLET-IMAGE
    networkID: NETWORK-ID
  debugSettings:
    enabled: false
    sshkeyName: ske-key
  selector:
    matchLabels:
      yawol.stackit.cloud/loadbalancer: lb1
```
4. export needed environment variables
```bash
export CLUSTER_NAMESPACE=default # namespace in which the controller works
export API_ENDPOINT=<externalip/url that the yawollet needs to connect> # externalip/url for kubernetes api server where yawollet can connect (incl. https://)
```

#### run

1. run yawol-controller
```bash
make run
#or
go run ./main.go
```

### debugging

1. upload ssh key-pair to openstack
2. add the following to `LoadBalancerMachine`
```yaml
...
spec:
  debugSettings:
    enabled: true
    sshkeyName: ssh-key-name
...
```
3. ssh onto the virtual machine with the username `alpine`

## yawollet

yawollet is a part of the yawol loadbalancer solution. yawollet is running on a vm to configure envoy from with the infos of a crd (`LoadBalancer`).


### (local) dev-setup
#### requirements

- any kind of kubernetes cluster (local or remote)
  -  for example: create kind cluster `kind create cluster`
- envoy locally installed (can be downloaded at: https://github.com/tetratelabs/archive-envoy/releases)
  - `make get-envoy` installs `envoy` on linux and macOS

#### preparation

1. generate the current yawol crds and install them in the cluster
```bash
make install
```

2. Create `LoadBalancer` and `LoadBalancerMachine` object (examples in example-setup folder)

*This example add an TCP LoadBalancer to forward port 8085 to localhost:9000 which is the envoy admin port*

`LoadBalancer`
```yaml
apiVersion: yawol.stackit.cloud/v1beta1
kind: LoadBalancer
metadata:
  name: loadbalancer-sample
spec:
  selector:
    matchLabels:
      yawol.stackit.cloud/loadbalancer: test
  replicas: 1
  endpoints: # defines endpoints for lb. In this case the just localhost
    - name: node1
      addresses:
        - 127.0.0.1
  ports: # define ports which cloud be forwarded to in this case from 8085 to 9000
    - name: http
      protocol: TCP
      port: 8085
      nodePort: 9000
  infrastructure: # is not used by yawollet can be set to everything
    networkID: none
    authSecretRef:
      name: none
      namespace: none
```

`LoadBalancerMachine`
```yaml
apiVersion: yawol.stackit.cloud/v1beta1
kind: LoadBalancerMachine
metadata:
  name: loadbalancermachine-sample
spec:
  infrastructure: # is not used by yawollet can be set to everything
    networkID: none
    authSecretRef:
      name: none
      namespace: none
  floatingID: none # is not used by yawollet can be set to everything
  portID: none # is not used by yawollet can be set to everything
  loadBalancerRef:
    name: loadbalancer-sample
    namespace: default
```

#### run

1. start envoy
```bash
envoy -c image/envoy-config.yaml
```
2. run yawollet *(yawollet is use the default kubeconfig if you want a different kubeconfig you can set it withe the `-kubeconfig` flag)*
```bash
make run
# or
go run main.go -namespace=default -loadbalancer-name=loadbalancer-sample -loadbalancer-machine-name=loadbalancermachine-sample
```

#### manual testing

UDP testing with netcat:
1. `kubectl apply -f example-setup/lb.yaml && kubectl apply -f example-setup/lbm.yaml`
2. `netcat -u -l 9001`
3. open a new terminal
4. `netcat -u 127.0.0.1 8086`
5. type something, hit enter and check if the message gets displayed in the first terminal

TCP testing using the admin port of envoy:
1. `kubectl apply -f example-setup/lb.yaml && kubectl apply -f example-setup/lbm.yaml`
2. open http://localhost:8085 in your browser
3. you should get forwarded to the admin port of envoy which is listening to localhost:9000

### debugging on openstack vm

1. upload ssh key-pair to openstack
2. add the following to `LoadBalancerMachine`
```yaml
...
spec:
  debugSettings:
    enabled: true
    sshkeyName: ssh-key-name
...
```

3. ssh onto the virtual machine with the username `alpine`  
