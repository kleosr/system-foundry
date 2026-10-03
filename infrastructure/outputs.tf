output "app_url" {
  value = "http://${var.bind_ip}:${local.app_port}"
}

output "network" {
  value = docker_network.pull_request.name
}
