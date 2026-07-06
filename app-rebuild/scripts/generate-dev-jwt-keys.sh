#!/usr/bin/env bash
set -euo pipefail

output_dir="${1:-.local-jwt}"
mkdir -p "$output_dir"

openssl genpkey -algorithm Ed25519 -out "$output_dir/private.pem"
openssl pkey -in "$output_dir/private.pem" -pubout -out "$output_dir/public.pem"
chmod 600 "$output_dir/private.pem"

echo "Development Ed25519 JWT keys written to $output_dir"
