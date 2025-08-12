# static-ct-staging

This directory contains Terragrunt configs for the `static-ct-staging` GCP project.

The [./logs](./logs/) directory contains the configs for the [Arche staging logs](/README.md#test_tube-public-test-instances).

The [./cloudbuild](./cloudbuild/) directory contains the configs for building
the TesseraCT binary, and deploying it automatically to logs under
[./logs](`./logs`) by applying their Terragrunt configuration.

The [./preloaders](./preloaders/) directory contains config for running [preloaders](https://github.com/google/certificate-transparency-go/blob/master/preload/preloader/preloader.go),
populating Arche logs with entries from [Argon logs](https://bugs.chromium.org/p/chromium/issues/detail?id=889033).

The [./loadbalancer](./loadbalancer/) directory contains config for running
Arche logs behind global public load balancer.
