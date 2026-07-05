#!/usr/bin/env bash
set -euo pipefail

output_dir="${1:-.local-certs}"
client_id="spiffe://meme-launchpad/internal-client"

mkdir -p "$output_dir"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$output_dir/ca.key"
openssl req -x509 -new -sha256 -days 3650 \
  -key "$output_dir/ca.key" \
  -subj "/CN=MEME Launchpad Development CA" \
  -out "$output_dir/ca.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$output_dir/server.key"
openssl req -new -sha256 \
  -key "$output_dir/server.key" \
  -subj "/CN=meme-api" \
  -addext "subjectAltName=DNS:meme-api,DNS:localhost,IP:127.0.0.1" \
  -addext "extendedKeyUsage=serverAuth" \
  -out "$output_dir/server.csr"
openssl x509 -req -sha256 -days 365 \
  -in "$output_dir/server.csr" \
  -CA "$output_dir/ca.crt" \
  -CAkey "$output_dir/ca.key" \
  -CAcreateserial \
  -copy_extensions copy \
  -out "$output_dir/server.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$output_dir/internal-client.key"
openssl req -new -sha256 \
  -key "$output_dir/internal-client.key" \
  -subj "/CN=internal-client" \
  -addext "subjectAltName=URI:$client_id" \
  -addext "extendedKeyUsage=clientAuth" \
  -out "$output_dir/internal-client.csr"
openssl x509 -req -sha256 -days 365 \
  -in "$output_dir/internal-client.csr" \
  -CA "$output_dir/ca.crt" \
  -CAkey "$output_dir/ca.key" \
  -CAcreateserial \
  -copy_extensions copy \
  -out "$output_dir/internal-client.crt"

chmod 600 "$output_dir/ca.key" "$output_dir/server.key" "$output_dir/internal-client.key"

echo "Development mTLS certificates written to $output_dir"
echo "Allowed client identity: $client_id"
