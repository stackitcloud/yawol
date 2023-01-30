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

To create the necessary environment to build the image, you can use the terraform code located within `hack/packer-infrastructure`.
Run `terraform init && terraform apply` within that directory. The output should contain all openstack specific IDs required
to build the image. After you are done, you can remove the build infrastructure via running `terraform destroy`

Before running our `Earthly` targets, set the needed environment variables:

```shell
export OS_NETWORK_ID=<from your openstack environment>
export OS_FLOATING_NETWORK_ID=<from your openstack environment>
export OS_SECURITY_GROUP_ID=<from your openstack environment>
export OS_SOURCE_IMAGE=<from your openstack environment>
export IMAGE_VISIBILITY=<private or public> 
```

To be able to login to OpenStack make sure you source your OpenStack Credentials. The following OpenSTack ENV variables are needed to build the image: `OS_AUTH_URL` `OS_PROJECT_ID` `OS_PROJECT_NAME` `OS_USER_DOMAIN_NAME` `OS_PASSWORD` `OS_USERNAME` `OS_REGION_NAME`

Then validate and build the image:

```shell
earthly +validate-yawollet-image
```

```shell
earthly +build-yawollet-image \
   --OS_NETWORK_ID="$OS_NETWORK_ID" \
   --OS_FLOATING_NETWORK_ID="$OS_FLOATING_NETWORK_ID" \
   --OS_SECURITY_GROUP_ID="$OS_SECURITY_GROUP_ID" \
   --OS_SOURCE_IMAGE="$OS_SOURCE_IMAGE" \
   --IMAGE_VISIBILITY="$IMAGE_VISIBILITY" \
   --OS_AUTH_URL="$OS_AUTH_URL" \
   --OS_PROJECT_ID="$OS_PROJECT_ID" \
   --OS_PROJECT_NAME="$OS_PROJECT_NAME" \
   --OS_USER_DOMAIN_NAME="$OS_USER_DOMAIN_NAME" \
   --OS_PASSWORD="$OS_PASSWORD" \
   --OS_USERNAME="$OS_USERNAME" \
   --OS_REGION_NAME="$OS_REGION_NAME"
```

### Cluster Installation

The in-cluster components of yawol (`yawol-cloud-controller` and
`yawol-controller`) can now be installed.

1. Make sure that `VerticalPodAutoscaler` is installed in the cluster.
2. Create a Kubernetes `Secret` that contains the contents of an `.openrc`
   file underneath the `cloudprovider.conf` key. The `.openrc` credentials need
   the correct permission to be able to create instances and request floating
   IPs.

**Note**: At most one of `domain-id` or `domain-name` and `project-id` or `project-name` must be provided.

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
       domain-id=""
       # Deprecated (tenant-name): Please use project-name
       tenant-name=""
       project-name=""
       project-id=""
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
    # Override the default  OpenStack image ID.
    yawol.stackit.cloud/imageId: "OS-imageId"
    # Override the default OpenStack machine flavor.
    yawol.stackit.cloud/flavorId: "OS-flavorId"
    # Overwrites the default openstack network for the loadbalancer.
    # If this is set to a different network ID than defined as default in the yawol-cloud-controller
    # the default from the yawol-cloud-controller will be added to the additionalNetworks.
    yawol.stackit.cloud/defaultNetworkID: "OS-networkID"
    # If set to true it do not add the default network ID from
    # the yawol-cloud-controller to the additionalNetworks.
    yawol.stackit.cloud/skipCloudControllerDefaultNetworkID: "false"
    # Overwrites the projectID which is set by the secret.
    # If not set the settings from the secret binding will be used.
    # This field is immutable and can not be changed after the service is created.
    yawol.stackit.cloud/projectID: "OS-ProjectID"
    # Overwrites the openstack floating network for the loadbalancer.
    yawol.stackit.cloud/floatingNetworkID: "OS-floatingNetID"
    # Override the default OpenStack availability zone.
    yawol.stackit.cloud/availabilityZone: "OS-AZ"
    # Specify if this should be an internal LoadBalancer .
    yawol.stackit.cloud/internalLB: "false"
    # Run yawollet in debug mode.
    yawol.stackit.cloud/debug: "false"
    # Reference the name of the SSH key provided to OpenStack for debugging .
    yawol.stackit.cloud/debugsshkey: "OS-keyName"
    # Allows filtering services in cloud-controller.
    yawol.stackit.cloud/className: "test"
    # Specify the number of LoadBalancer machines to deploy (default 1).
    yawol.stackit.cloud/replicas: "3"
    # Specify an existing floating IP for yawol to use.
    yawol.stackit.cloud/existingFloatingIP: "193.148.175.46"
    # Enable/disable envoy support for proxy protocol.
    yawol.stackit.cloud/tcpProxyProtocol: "false"
    # Defines proxy protocol ports (comma separated list).
    yawol.stackit.cloud/tcpProxyProtocolPortsFilter: "80,443"
    # Enables log forwarding.
    yawol.stackit.cloud/logForward: "true"
    # Defines loki URL for the log forwarding.
    yawol.stackit.cloud/logForwardLokiURL: "http://example.com:3100/loki/api/v1/push"
    # Defines the TCP idle Timeout as duration, default is 1h.
    # Make sure there is a valid unit (like "s", "m", "h"), otherwise this option is ignored.
    yawol.stackit.cloud/tcpIdleTimeout: "5m30s"
    # Defines the UDP idle Timeout as duration, default is 1m.
    # Make sure there is a valid unit (like "s", "m", "h"), otherwise this option is ignored.
    yawol.stackit.cloud/udpIdleTimeout: "5m"
    # Defines the openstack server group policy for a LoadBalancer.
    # Can be 'affinity', 'anti-affinity' 'soft-affinity', 'soft-anti-affinity' depending on the OpenStack Infrastructure.
    # If not set openstack server group is disabled.
    yawol.stackit.cloud/serverGroupPolicy: anti-affinity
    # Defines additional openstack networks for the loadbalancer (comma separated list).
    yawol.stackit.cloud/additionalNetworks: "OS-networkID1,OS-networkID2"
```

See [our example service](example-setup/yawol-cloud-controller/service.yaml)
for an overview.

## Development

See the [development guide](docs/development.md).
