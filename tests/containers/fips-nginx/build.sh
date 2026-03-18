#!/usr/bin/env bash

# Run the image using command:
# podman run -d --name fips-nginx -p 8443:8443 fips-nginx
set -euo pipefail

# Check if argument is provided
if [ $# -eq 0 ]; then
    echo "Error: Missing IP address argument"
    echo "Usage: $0 <ip-address>"
    exit 1
fi

ip="$1"

tmpdir="$(mktemp -d)"
trap 'rm -rf -- "${tmpdir}"' EXIT

cp Containerfile "${tmpdir}"
cd ${tmpdir}

# Prepare index.html
cat <<EOF > index.html
This file was served from an RHCOS FIPS-hardened server.
EOF

# Prepare nginx.conf
cat <<EOF > nginx.conf
events {}

http {
    server {
        listen 8443 ssl;
        server_name _;

        # ---- FIPS-only TLS ----
        ssl_protocols TLSv1.2;
        ssl_prefer_server_ciphers on;

        ssl_ciphers ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256;

        ssl_certificate     /etc/nginx/tls/fips-server.crt;
        ssl_certificate_key /etc/nginx/tls/fips-server.key;

        location / {
            root /usr/share/nginx/html;
            index index.html;
        }
    }
}
EOF

mkdir -p tls
pushd tls/
# Prepare openssl.cnf
# The IP must point to an nginx server configured with FIPS-compliant ciphers
cat <<SSLEOF > openssl.cnf
[ req ]
default_bits        = 3072
distinguished_name  = dn
prompt              = no
string_mask         = utf8only
req_extensions      = req_ext

[ dn ]
CN = FIPS TLS Test Server

[ req_ext ]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = critical, serverAuth
subjectAltName = @alt_names

[ alt_names ]
IP.1 = ${ip}
SSLEOF

# Prepare key and crt
## Generate the private key (FIPS-approved)
openssl genpkey \
  -algorithm RSA \
  -pkeyopt rsa_keygen_bits:3072 \
  -out fips-server.key

## Generate CSR (still FIPS-only)
openssl req -new -key fips-server.key -out fips-server.csr -config openssl.cnf

## Self-sign the certificate (TLS-compatible + FIPS)
openssl x509 -req \
  -in fips-server.csr \
  -signkey fips-server.key \
  -out fips-server.crt \
  -days 3650 \
  -sha256 \
  -extfile openssl.cnf \
  -extensions req_ext

# Verify SAN present
openssl x509 -in fips-server.crt -noout -text | grep -A2 "Subject Alternative Name"

openssl verify \
  -provider fips \
  -CAfile fips-server.crt \
  fips-server.crt

rm fips-server.csr openssl.cnf

popd

podman build -t fips-nginx .
