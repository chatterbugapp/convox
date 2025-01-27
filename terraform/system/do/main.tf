terraform {
  required_version = ">= 0.12.0"
}

provider "digitalocean" {
  version = "~> 1.10"
}

provider "http" {
  version = "~> 1.1"
}

provider "kubernetes" {
  version = "~> 1.9"

  cluster_ca_certificate = module.cluster.ca
  host                   = module.cluster.endpoint
  token                  = module.cluster.token

  load_config_file = false
}

data "http" "releases" {
  url = "https://api.github.com/repos/convox/convox/releases"
}

locals {
  current = jsondecode(data.http.releases.body).0.tag_name
  release = coalesce(var.release, local.current)
}

module "cluster" {
  source = "../../cluster/do"

  providers = {
    digitalocean = digitalocean
  }

  name      = var.name
  node_type = var.node_type
  region    = var.region
  token     = var.token
}

module "fluentd" {
  source = "../../fluentd/do"

  providers = {
    kubernetes = kubernetes
  }

  cluster       = module.cluster.name
  elasticsearch = module.rack.elasticsearch
  namespace     = "kube-system"
  name          = var.name
}

module "rack" {
  source = "../../rack/do"

  providers = {
    digitalocean = digitalocean
    kubernetes   = kubernetes
  }

  access_id     = var.access_id
  cluster       = module.cluster.name
  name          = var.name
  region        = var.region
  registry_disk = var.registry_disk
  release       = local.release
  secret_key    = var.secret_key
}
