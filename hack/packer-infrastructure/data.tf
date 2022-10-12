data "openstack_networking_network_v2" "public" {
  name = var.floating_ip_network_name
}

data "openstack_images_image_v2" "alpine" {
  properties = {
    os_distro = "alpine"
  }
  most_recent = true
}
