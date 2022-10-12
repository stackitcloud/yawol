output "OS_NETWORK_ID" {
  value = openstack_networking_network_v2.packer.id
}

output "OS_FLOATING_NETWORK_ID" {
  value = data.openstack_networking_network_v2.public.id
}

output "OS_SECURITY_GROUP_ID" {
  value = openstack_networking_secgroup_v2.packer.id
}

output "OS_SOURCE_IMAGE" {
  value = data.openstack_images_image_v2.alpine.id
}
