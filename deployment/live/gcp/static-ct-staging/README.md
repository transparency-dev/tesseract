# static-ct-staging

This directory contains Terragrunt configs for the `static-ct-staging` GCP project.

The [./logs](./logs/) directory contains configs for [Arche logs](/README.md#test_tube-public-test-instances).

The [./cloudbuild](./cloudbuild/) directory contains configs for building
the TesseraCT binary, and deploying it automatically to logs under
[./logs](`./logs`) by applying their Terragrunt configuration.

The [./preloaders](./preloaders/) directory contains configs for running [preloaders](https://github.com/google/certificate-transparency-go/blob/master/preload/preloader/preloader.go),
populating Arche logs with entries from [Argon logs](https://bugs.chromium.org/p/chromium/issues/detail?id=889033).

The [./loadbalancer](./loadbalancer/) directory contains configs for running
a global public load balancer in front of Arche logs.

## Howto

### Make a log public

To make a log public, you need to both:

 1. Make its bucket public with the [`public_bucket` attribute](/deployment/modules/gcp/tesseract/gce/variables.tf)
set to `true` in the [log's Terragrunt configuration](./logs/). You may need to
disable [Public Access Prevention](https://cloud.google.com/storage/docs/public-access-prevention)
first.
 2. Configure the Global Load Balancer with this log, by adding the corresponding
 log name in [./loadbalancer/terragrunt.hcl](./loadbalancer/terragrunt.hcl), and
 apply the config. It may take up to an hour for TLS certs to be provisionned,
 and for endpoints to be available over HTTPS.

### Make a log private

To make a log private, you need to both:

 1. Make its bucket private with the [`public_bucket` attribute](/deployment/modules/gcp/tesseract/gce/variables.tf)
set to `false` in the [log's Terragrunt configuration](./logs/).
Alternatively, switch [Public Access Prevention](https://cloud.google.com/storage/docs/public-access-prevention)
on in Pantheon.
 2. Configure the Global Load Balancer without this log, by removing the corresponding
log name in [./loadbalancer/terragrunt.hcl](./loadbalancer/terragrunt.hcl),
and applying the config.  Due to a [dependency loop](https://github.com/terraform-google-modules/terraform-google-lb-http/issues/159)
between forwarding rules and the load balancer backend groups, Terragrunt might
not be able to apply the config. If you run into this go to the Load Balancer
page in Pantheon, and manually delete the corresponding forwarding rule and
backend group.
