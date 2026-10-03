terraform {
  required_version = ">= 1.6.0"
  backend "local" {}
  required_providers {
    docker = {
      source  = "kreuzwerker/docker"
      version = "~> 3.0"
    }
  }
}

provider "docker" {}
