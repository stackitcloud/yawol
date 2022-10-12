resource "openstack_networking_network_v2" "packer" {
  name           = "packer"
  admin_state_up = "true"
}

resource "openstack_networking_subnet_v2" "packer" {
  name       = "packer"
  network_id = openstack_networking_network_v2.packer.id
  cidr       = "192.168.48.0/24"
  ip_version = 4
}

resource "openstack_networking_router_v2" "packer" {
  name                = "packer"
  admin_state_up      = "true"
  external_network_id = data.openstack_networking_network_v2.public.id
}

resource "openstack_networking_router_interface_v2" "packer" {
  router_id = openstack_networking_router_v2.packer.id
  subnet_id = openstack_networking_subnet_v2.packer.id
}

resource "openstack_networking_secgroup_v2" "packer" {
  name        = "packer"
  description = "Security Group for packer builds"
}

resource "openstack_networking_secgroup_rule_v2" "packer-ssh" {
  direction         = "ingress"
  ethertype         = "IPv4"
  protocol          = "tcp"
  port_range_min    = 22
  port_range_max    = 22
  remote_ip_prefix  = "0.0.0.0/0"
  security_group_id = openstack_networking_secgroup_v2.packer.id
}
