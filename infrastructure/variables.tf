variable "pr_number" {
  type = number
}

variable "postgres_password" {
  type      = string
  sensitive = true
}

variable "image_tag" {
  type    = string
  default = "local"
}

variable "app_password" {
  type      = string
  sensitive = true
}

variable "signing_key_pem" {
  type      = string
  sensitive = true
}

variable "operator_password" {
  type      = string
  sensitive = true
  validation {
    condition     = length(var.operator_password) >= 12
    error_message = "operator_password must have at least 12 characters."
  }
}

variable "webhook_secret" {
  type      = string
  sensitive = true
}

variable "bind_ip" {
  type    = string
  default = "127.0.0.1"
}

locals {
  prefix   = "foundry-pr-${var.pr_number}"
  app_port = 20000 + var.pr_number
}
