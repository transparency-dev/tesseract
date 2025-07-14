# AWS live configs

This directory contains Terragrunt configs we use to run TesseraCT logs and
other related pieces of infrastructure:

- `test`: configures a test log using a
EC2 VM
- `ci`: configures a conformance log with the [hammer](/internal/hammer/)
for continuous integration testing
