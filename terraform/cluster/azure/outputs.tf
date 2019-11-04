output "kubeconfig" {
  depends_on = [
    local_file.kubeconfig,
    azurerm_kubernetes_cluster.rack,
  ]
  value = local_file.kubeconfig.filename
}
