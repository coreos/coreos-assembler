# fips-nginx Container

This is used by the `fips.enable.https` test to verify that using
TLS works in FIPS mode by having Ignition fetch a remote resource
over HTTPS with FIPS compatible algorithms.

See https://catalog.redhat.com/en/software/containers/rhel10/nginx-126/677d3718e58b5a1ae5598058#overview

To build the container using command:
`./build.sh <IP>`

To run the container image using command:
`podman run -d -p 8443:8443 --name fips-nginx fips-nginx`

Remember to create firewall-rules to allow port 8443:
```
gcloud compute firewall-rules create allow-nginx-fips-8443 \
    --action ALLOW \
    --direction INGRESS \
    --rules tcp:8443 \
    --source-ranges 0.0.0.0/0 \
    --target-tags nginx-fips-server \
    --description "Allow FIPS test access to nginx on port 8443"

gcloud compute instances add-tags rhcos-fips-test \
    --zone us-central1-a \
    --tags nginx-fips-server
```
