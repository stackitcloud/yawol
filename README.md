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

We provide a [base image](https://github.com/stackitcloud/alpine-openstack-image) 
that uses Alpine. We are currently working on `Makefile` targets to simplify the
creation of the image.

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

After successfull installation, you can request `Services` of
`type: LoadBalancer` and yawol will take care of creating an instance,
allocating an IP, and updating the `Service` resource once the setup is ready.

## Development

See the [development guide](docs/development.md).
