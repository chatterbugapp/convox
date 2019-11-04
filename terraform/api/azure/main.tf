terraform {
  required_version = ">= 0.12.0"
}

provider "azurerm" {
  version = "~> 1.36"
}

provider "kubernetes" {
  version = "~> 1.8"

  config_path = var.kubeconfig
}

locals {
  tags = {
    System = "convox"
    Rack   = var.name
  }
}

data "azurerm_resource_group" "rack" {
  name = var.resource_group
}

resource "random_string" "suffix" {
  length  = 12
  special = false
  upper   = false
}

module "k8s" {
  source = "../k8s"

  providers = {
    kubernetes = kubernetes
  }

  domain     = var.domain
  kubeconfig = var.kubeconfig
  name       = var.name
  namespace  = var.namespace
  release    = var.release

  annotations = {}

  env = {
    BUCKET = azurerm_storage_container.storage.name
    # ELASTICSEARCH_URL = "http://elasticsearch.kube-system.svc.cluster.local:9200"
    PROVIDER = "azure"
    REGION   = var.region
    REGISTRY = azurerm_container_registry.registry.login_server
    ROUTER   = var.router
  }
}
