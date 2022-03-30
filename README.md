# yawol - yet another working openstack loadbalancer

yawol ia a loadbalancer solution for openstack, based on the kubernetes controller pattern.

yawol uses kubebuilder as k8s controller framework and gophercloud for the openstack integration

## run tests

### requirements

* install kubebuilder **v3**

### run

```bash
make test
```
example output:
```
go test ./... -coverprofile cover.out
?       github.com/stackitcloud/yawol-cloud-controller  [no test files]
ok      github.com/stackitcloud/yawol-cloud-controller/controllers/control_controller   10.615s coverage: 79.5% of statements
ok      github.com/stackitcloud/yawol-cloud-controller/controllers/target_controller    22.419s coverage: 73.2% of statements
```

## yawol-cloud-controller

yawol-cloud-controller is a part of the yawol loadbalancer solution. yawol-cloud-controller translate information from a kubernetes `Service` and kubernetes `Node` into a `LoadBalancer`.

## Controllers

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
