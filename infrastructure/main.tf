resource "docker_network" "pull_request" {
  name = local.prefix
}

resource "docker_image" "postgres" {
  name = "pgvector/pgvector:pg16"
}

resource "docker_image" "nats" {
  name = "nats:2.10"
}

resource "docker_image" "redis" {
  name = "redis:7"
}

resource "docker_image" "temporal" {
  name = "temporalio/temporal:latest"
}

resource "docker_container" "postgres" {
  name    = "${local.prefix}-postgres"
  image   = docker_image.postgres.image_id
  restart = "unless-stopped"
  env = [
    "POSTGRES_USER=foundry_admin",
    "POSTGRES_DB=foundry",
    "POSTGRES_PASSWORD=${var.postgres_password}",
    "FOUNDRY_APP_PASSWORD=${var.app_password}",
  ]
  upload {
    content = file("${path.module}/../search/sql/20261002_documents.sql")
    file    = "/docker-entrypoint-initdb.d/001_documents.sql"
  }
  upload {
    content = file("${path.module}/../auth/sql/20261003_credentials.sql")
    file    = "/docker-entrypoint-initdb.d/002_credentials.sql"
  }
  upload {
    content    = file("${path.module}/postgres/003_app_login.sh")
    file       = "/docker-entrypoint-initdb.d/003_app_login.sh"
    executable = true
  }
  healthcheck {
    test     = ["CMD", "pg_isready", "-U", "foundry_admin", "-d", "foundry"]
    interval = "2s"
    retries  = 30
  }
  networks_advanced {
    name = docker_network.pull_request.name
  }
}

resource "docker_container" "nats" {
  name    = "${local.prefix}-nats"
  image   = docker_image.nats.image_id
  restart = "unless-stopped"
  command = ["-js", "-m", "8222"]
  networks_advanced {
    name = docker_network.pull_request.name
  }
}

resource "docker_container" "redis" {
  name    = "${local.prefix}-redis"
  image   = docker_image.redis.image_id
  restart = "unless-stopped"
  networks_advanced {
    name = docker_network.pull_request.name
  }
}

resource "docker_container" "temporal" {
  name    = "${local.prefix}-temporal"
  image   = docker_image.temporal.image_id
  restart = "unless-stopped"
  command = ["server", "start-dev", "--ip", "0.0.0.0", "--headless"]
  networks_advanced {
    name = docker_network.pull_request.name
  }
}

locals {
  shared_env = [
    "FOUNDRY_POSTGRES_DSN=postgres://foundry_app:${var.app_password}@${local.prefix}-postgres:5432/foundry?sslmode=disable",
    "FOUNDRY_NATS_URL=nats://${local.prefix}-nats:4222",
    "FOUNDRY_TEMPORAL_ADDR=${local.prefix}-temporal:7233",
  ]
}

resource "docker_container" "app" {
  name    = "${local.prefix}-app"
  image   = "systemsfoundry:${var.image_tag}"
  restart = "unless-stopped"
  env = concat(local.shared_env, [
    "FOUNDRY_MODE=shared",
    "FOUNDRY_HTTP_ADDR=0.0.0.0:8080",
    "FOUNDRY_ORIGIN=http://${var.bind_ip}:${local.app_port}",
    "FOUNDRY_REDIS_ADDR=${local.prefix}-redis:6379",
    "FOUNDRY_SIGNING_KEY_FILE=/run/foundry/signing.pem",
    "FOUNDRY_BOOTSTRAP_OPERATOR_EMAIL=operator@ops.test",
    "FOUNDRY_BOOTSTRAP_OPERATOR_PASSWORD_FILE=/run/foundry/operator-password",
    "FOUNDRY_WEBHOOK_SECRET=${var.webhook_secret}",
    "FOUNDRY_CHAOS_ENABLED=true",
  ])
  upload {
    content = var.signing_key_pem
    file    = "/run/foundry/signing.pem"
  }
  upload {
    content = var.operator_password
    file    = "/run/foundry/operator-password"
  }
  ports {
    internal = 8080
    external = local.app_port
    ip       = var.bind_ip
  }
  networks_advanced {
    name = docker_network.pull_request.name
  }
  depends_on = [
    docker_container.postgres,
    docker_container.nats,
    docker_container.redis,
    docker_container.temporal,
  ]
}

resource "docker_container" "orders_worker" {
  name       = "${local.prefix}-orders-worker"
  image      = "systemsfoundry:${var.image_tag}"
  restart    = "unless-stopped"
  entrypoint = ["/orders-worker"]
  env        = local.shared_env
  networks_advanced {
    name = docker_network.pull_request.name
  }
  depends_on = [docker_container.nats]
}

resource "docker_container" "onboarding_worker" {
  name       = "${local.prefix}-onboarding-worker"
  image      = "systemsfoundry:${var.image_tag}"
  restart    = "unless-stopped"
  entrypoint = ["/onboarding-worker"]
  env        = local.shared_env
  networks_advanced {
    name = docker_network.pull_request.name
  }
  depends_on = [docker_container.postgres, docker_container.temporal]
}
