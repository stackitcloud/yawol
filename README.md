<p align="center">
  <img src="docs/logo.svg" alt="yawol">
</p>

<p align="center">
    <em>Do OpenStack Load Balancing the Kubernetes Way.</em>
</p>

****

yawol (**y**et **a**nother **w**orking **O**penStack **L**oad Balancer) is a
Load Balancer solution for OpenStack, based on the Kubernetes controller
pattern.

****

## Key Features

* Replacement for OpenStack Octavia Load Balancing
* Provides Load Balancers for Kubernetes `Services`
* Fully manages the instance lifecycle of Load Balancer VMs
* Kubernetes-native approach: All the benefits of CRDs and controllers

## How It Works

yawol uses [kubebuilder](https://kubebuilder.io/) as the controller
framework and [gophercloud](https://github.com/gophercloud/gophercloud) for the
OpenStack integration. The actual load balancing is done by
[Envoy](https://www.envoyproxy.io/).

For a more in-detail description, see [the components documentation](docs/components.md).

## Installation

> If this installation guide doesn't work for you, or if some instructions are
> unclear, please open an issue!

We provide a Helm chart for yawol in [charts/yawol-controller](charts/yawol-controller/)
that you can use for a quick installation on a Kubernetes cluster. In order to
get yawol going, however, you need a yawol OpenStack VM image first.

### yawol OpenStack Image

We use an openstack alpine base image which can be created with this
[packer file](https://github.com/stackitcloud/alpine-openstack-image).

Before running our `Makefile` targets, set the needed environment variables:

```shell
export OS_PROJECT_ID=<from your openstack environment>
export OS_SOURCE_IMAGE=<from your openstack environment>
export OS_NETWORK_ID=<from your openstack environment>
export OS_FLOATING_NETWORK_ID=<from your openstack environment>
export OS_SECURITY_GROUP_ID=<from your openstack environment>
export SOURCE_VERSION=1 # provided by your CI
export BUILD_NUMBER=1 # provided by your CI
export YAWOLLET_VERSION=1 # provided by your CI
export BUILD_TYPE=release # one of {release|feature}
```

To create the necessary environment to build the image, you can use the terraform code located within `hack/packer-infrastructure`.
Run `terraform init && terraform apply` within that directory. The output should contain all openstack specific IDs required
to build the image. After you are done, you can remove the build infrastructure via running `terraform destroy` 

Then validate and build the image:

```shell
make validate-image-yawollet
```

```shell
make build-image-yawollet
```

### Cluster Installation

The in-cluster components of yawol (`yawol-cloud-controller` and
`yawol-controller`) can now be installed.

1. Make sure that `VerticalPodAutoscaler` is installed in the cluster.
2. Create a Kubernetes `Secret` that contains the contents of an `.openrc`
   file underneath the `cloudprovider.conf` key. The `.openrc` credentials need
   the correct permission to be able to create instances and request floating
   IPs.

   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: cloud-provider-config
   type: Opaque
   stringData:
     cloudprovider.conf: |-
       [Global]
       auth-url="""
       domain-name=""
       tenant-name=""
       project-name=""
       username=""
       password=""
       region=""
   ```

   Assuming you saved the secret as `secret-cloud-provider-config.yaml`, apply
   it with:

   ```shell
   kubectl apply -f secret-cloud-provider-config.yaml
   ```

3. Configure the [Helm values](charts/yawol-controller/values.yaml) according to
   your OpenStack environment:
   
   **Values for the yawol-cloud-controller**

   ```yaml
   # the name of the Kubernetes secret we created in the previous step
   #
   # Placed in LoadBalancer.spec.infrastructure.authSecretRef.name
   yawolOSSecretName: cloud-provider-config

   # floating IP ID of the IP pool that yawol uses to request IPs
   #
   # Placed in LoadBalancer.spec.infrastructure.floatingNetID
   yawolFloatingID: <floating-id>

   # OpenStack network ID in which the Load Balancer is placed
   #
   # Placed in LoadBalancer.spec.infrastructure.networkID
   yawolNetworkID: <network-id>

   # default value for flavor that yawol Load Balancer instances should use
   # can be overridden by annotation
   #
   # Placed in LoadBalancer.spec.infrastructure.flavor.flavor_id
   yawolFlavorID: <flavor-id>

   # default value for ID of the image used for the Load Balancer instance
   # can be overridden by annotation
   #
   # Placed in LoadBalancer.spec.infrastructure.image.image_id
   yawolImageID: <image-id>

   # default value for the AZ used for the Load Balancer instance
   # can be overridden by annotation. If not set, empty string is used.
   #
   # Placed in LoadBalancer.spec.infrastructure.availabilityZone
   yawolAvailabilityZone: <availability-zone>
   ```

   **Values for the yawol-controller**

   ```yaml
   # URL/IP of the Kubernetes API server that contains the LoadBalancer resources
   yawolAPIHost: <api-host>
   ```

3. With the values correctly configured, you can now install the Helm chart.

   ```shell
   helm install yawol ./charts/yawol-controller
   ```

   This will also install the CRDs needed by yawol.

After successful installation, you can request `Services` of
`type: LoadBalancer` and yawol will take care of creating an instance,
allocating an IP, and updating the `Service` resource once the setup is ready.

You can also specify custom annotations on the `Service` to further control the
behavior of yawol.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: loadbalancer
  annotations:
    # override the default  OpenStack image ID
    yawol.stackit.cloud/imageId: "OS-imageId"
    # override the default OpenStack machine flavor
    yawol.stackit.cloud/flavorId: "OS-flavorId"
    # override the default OpenStack availability zone
    yawol.stackit.cloud/availabilityZone: "OS-AZ"
    # specify if this should be an internal LoadBalancer 
    yawol.stackit.cloud/internalLB: "false"
    # run yawollet in debug mode
    yawol.stackit.cloud/debug: "false"
    # reference the name of the SSH key provided to OpenStack for debugging 
    yawol.stackit.cloud/debugsshkey: "OS-keyName"
    # allows filtering services in cloud-controller
    yawol.stackit.cloud/className: "test"
    # specify the number of LoadBalancer machines to deploy (default 1)
    yawol.stackit.cloud/replicas: "3"
    # specify an existing floating IP for yawol to use
    yawol.stackit.cloud/existingFloatingIP: "193.148.175.46"
    # enable/disable envoy support for proxy protocol
    yawol.stackit.cloud/tcpProxyProtocol: "false"
    # defines proxy protocol ports (comma separated list)
    yawol.stackit.cloud/tcpProxyProtocolPortsFilter: "80,443"
    # enables log forwarding
    yawol.stackit.cloud/logForward: "true"
    # defines loki URL for the log forwarding
    yawol.stackit.cloud/logForwardLokiURL: "http://example.com:3100/loki/api/v1/push"
```

See [our example service](example-setup/yawol-cloud-controller/service.yaml)
for an overview.

## Development

See the [development guide](docs/development.md).
