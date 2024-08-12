# yawol-controller

![Version: 0.23.1-1](https://img.shields.io/badge/Version-0.23.1--1-informational?style=flat-square) ![AppVersion: v0.23.1](https://img.shields.io/badge/AppVersion-v0.23.1-informational?style=flat-square)

Helm chart for yawol-controller

## Source Code

* <https://github.com/stackitcloud/yawol>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| featureGates | object | `{}` |  |
| logging | object | `{"encoding":"console","level":"info","stacktraceLevel":"error"}` | values are passed as zap-flags to the containers. See https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/log/zap#Options.BindFlags for more information |
| logging.encoding | string | `"console"` | log encoding (one of 'json' or 'console') |
| logging.level | string | `"info"` | Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error' or any integer value > 0 which corresponds to custom debug levels of increasing verbosity |
| logging.stacktraceLevel | string | `"error"` | level at and above which stacktraces are captured (one of 'info', 'error' or 'panic') |
| namespace | string | `"kube-system"` |  |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| proxy | object | `{}` |  |
| replicas | int | `1` |  |
| resources.yawolCloudController.limits.cpu | string | `"500m"` |  |
| resources.yawolCloudController.limits.memory | string | `"512Mi"` |  |
| resources.yawolCloudController.requests.cpu | string | `"100m"` |  |
| resources.yawolCloudController.requests.memory | string | `"64Mi"` |  |
| resources.yawolControllerLoadbalancer.limits.cpu | string | `"500m"` |  |
| resources.yawolControllerLoadbalancer.limits.memory | string | `"512Mi"` |  |
| resources.yawolControllerLoadbalancer.requests.cpu | string | `"100m"` |  |
| resources.yawolControllerLoadbalancer.requests.memory | string | `"64Mi"` |  |
| resources.yawolControllerLoadbalancermachine.limits.cpu | string | `"500m"` |  |
| resources.yawolControllerLoadbalancermachine.limits.memory | string | `"512Mi"` |  |
| resources.yawolControllerLoadbalancermachine.requests.cpu | string | `"100m"` |  |
| resources.yawolControllerLoadbalancermachine.requests.memory | string | `"64Mi"` |  |
| resources.yawolControllerLoadbalancerset.limits.cpu | string | `"500m"` |  |
| resources.yawolControllerLoadbalancerset.limits.memory | string | `"512Mi"` |  |
| resources.yawolControllerLoadbalancerset.requests.cpu | string | `"100m"` |  |
| resources.yawolControllerLoadbalancerset.requests.memory | string | `"64Mi"` |  |
| vpa.enabled | bool | `false` |  |
| vpa.yawolCloudController.mode | string | `"Auto"` |  |
| vpa.yawolController.mode | string | `"Auto"` |  |
| yawolAPIHost | string | `nil` |  |
| yawolAvailabilityZone | string | `""` |  |
| yawolCloudController.additionalEnv | object | `{}` |  |
| yawolCloudController.clusterRoleEnabled | bool | `true` |  |
| yawolCloudController.enabled | bool | `true` |  |
| yawolCloudController.gardenerMonitoringEnabled | bool | `false` |  |
| yawolCloudController.image.repository | string | `"ghcr.io/stackitcloud/yawol/yawol-cloud-controller"` |  |
| yawolCloudController.image.tag | string | `""` | Allows you to override the yawol version in this chart. Use at your own risk. |
| yawolCloudController.service.annotations | object | `{}` |  |
| yawolCloudController.service.labels | object | `{}` |  |
| yawolCloudController.serviceAccount | object | `{}` |  |
| yawolController.errorBackoffBaseDelay | string | `"5ms"` |  |
| yawolController.errorBackoffMaxDelay | string | `"1000s"` |  |
| yawolController.gardenerMonitoringEnabled | bool | `false` |  |
| yawolController.image.repository | string | `"ghcr.io/stackitcloud/yawol/yawol-controller"` |  |
| yawolController.image.tag | string | `""` | Allows you to override the yawol version in this chart. Use at your own risk. |
| yawolController.service.annotations | object | `{}` |  |
| yawolController.service.labels | object | `{}` |  |
| yawolFlavorID | string | `nil` |  |
| yawolFloatingID | string | `nil` |  |
| yawolImageID | string | `nil` |  |
| yawolNetworkID | string | `nil` |  |
| yawolOSSecretName | string | `nil` |  |
| yawolSubnetID | string | `nil` |  |

