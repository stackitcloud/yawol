terraform {
  required_version = ">= 1.1.7"

  required_providers {
    openstack = {
      source  = "terraform-provider-openstack/openstack"
      version = ">= 1.47"
    }
  }
}

provider "openstack" {}
