variable "image_version" {
  type        = string
  description = "Version suffix for the resulting image"
}

variable "image_tags" {
  type        = list(string)
  description = "List of tags to add to the image."
}

variable "image_visibility" {
  type        = string
  description = "One of 'public', 'private', 'shared' or 'community' (imageservice.ImageVisibility)"
}

variable "os_project_id" {
  type        = string
  description = "The openstack project id where the VM is created for setup."
}

variable "network_id" {
  type        = string
  description = "The network id to be assigned to the VM during creation."
}

variable "floating_network_id" {
  type        = string
  description = "The ID or name of the external network that can be used for creation of a new floating IP."
}

variable "security_group_id" {
  type        = string
  description = "Security group name to add to the instance."
}

variable "machine_flavor" {
  type        = string
  default     = "c1.2"
  description = "The ID, name, or full URL for the desired flavor for the server to be created."
}

variable "source_image" {
  type        = string
  description = "The source image this build is based on, which must contain an alpine linux and match the machine_flavor above."
}

source "openstack" "yawollet" {
  external_source_image_properties = {
    os_distro = "alpine"
    os_type   = "linux"
  }
  flavor                   = var.machine_flavor
  floating_ip_network      = "${var.floating_network_id}"
  instance_floating_ip_net = "${var.network_id}"
  image_name               = "yawol-alpine-${var.image_version}"
  image_visibility         = "${var.image_visibility}"
  metadata = {
    os_distro = "alpine"
    os_type   = "linux"
  }
  tenant_id               = "${var.os_project_id}"
  networks                = ["${var.network_id}"]
  security_groups         = ["${var.security_group_id}"]
  source_image            = "${var.source_image}"
  ssh_username            = "alpine"
  use_blockstorage_volume = true
  volume_size             = 1
  volume_type             = "storage_premium_perf6"
  ssh_timeout             = "10m"
  image_tags              = var.image_tags
}

build {
  sources = ["source.openstack.yawollet"]

  provisioner "ansible" {
    playbook_file = "./image/install-alpine.yaml"
    use_proxy     = false
    user          = "alpine"
  }

}
