#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p .local
if [ ! -f .local/signing.pem ]; then
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out .local/signing.pem
fi
if [ ! -f .local/operator-password ]; then
  openssl rand -hex 16 > .local/operator-password
fi
chmod 644 .local/signing.pem .local/operator-password
if [ ! -f .env ]; then
  {
    echo "POSTGRES_PASSWORD=$(openssl rand -hex 16)"
    echo "FOUNDRY_APP_PASSWORD=$(openssl rand -hex 16)"
    echo "FOUNDRY_WEBHOOK_SECRET=$(openssl rand -hex 24)"
  } > .env
  chmod 600 .env
fi
echo "Wrote .env and .local/. The operator is operator@ops.test. Its password is in .local/operator-password."
