variable "floating_ip_network_name" {
  description = "Name of the network your floating IPs are hosted in"
}

variable "dns_servers" {
  default = [ "8.8.8.8" ]
  description = "List of DNS servers which are used for the subnet"
}
