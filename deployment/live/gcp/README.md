# GCP live configs

This directory contains Terragrunt configs we use to run static-ct-api logs and
other related pieces of infrastructure:

- [./static-ct](./static-ct/): configures a continuous integration environment using the [hammer](/internal/hammer/)
- [./static-ct-staging](./static-ct-staging): configures [Arche staging logs](/README.md#test_tube-public-test-instances)
- [./test](./static-ct-staging/): configures a test log using a GCP VM
