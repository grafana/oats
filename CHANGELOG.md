# Changelog

## [0.8.0](https://github.com/grafana/oats/compare/v0.7.0...v0.8.0) (2026-07-23)


### Features

* integrate OATs v2 rewrite ([#401](https://github.com/grafana/oats/issues/401)) ([19f6cbb](https://github.com/grafana/oats/commit/19f6cbb6c43b48c6c5a20896f24b96c30c298aed))


### Bug Fixes

* **cli:** restore --lgtm-version flag ([#430](https://github.com/grafana/oats/issues/430)) ([6682d85](https://github.com/grafana/oats/commit/6682d85491e83e5ad3276c6d72ae863f47d0727f))
* **cli:** restore positional config discovery ([#433](https://github.com/grafana/oats/issues/433)) ([3b4477a](https://github.com/grafana/oats/commit/3b4477ab15b842ffb0b7c71357412c5cacfbf7d6))
* **deps:** update module github.com/onsi/gomega to v1.42.0 ([#336](https://github.com/grafana/oats/issues/336)) ([8b98649](https://github.com/grafana/oats/commit/8b9864958f15ed5e1101d407945209e3c9910aa3))
* **deps:** update module github.com/onsi/gomega to v1.42.1 ([#353](https://github.com/grafana/oats/issues/353)) ([cb1fc89](https://github.com/grafana/oats/commit/cb1fc89ea691444a800d0e08ddfbeffad22c0bf8))
* **deps:** update module github.com/spf13/pflag to v1.0.10 ([#408](https://github.com/grafana/oats/issues/408)) ([a4139f8](https://github.com/grafana/oats/commit/a4139f8222069611b8c7d434b76813c26c397b11))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.60.0 ([#333](https://github.com/grafana/oats/issues/333)) ([daf4524](https://github.com/grafana/oats/commit/daf4524d6d9e0d5ce9b67cd7b7298a45e1d65c51))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.61.0 ([#352](https://github.com/grafana/oats/issues/352)) ([0687620](https://github.com/grafana/oats/commit/0687620f5a9301622651ef8965412703abf43014))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.62.0 ([#377](https://github.com/grafana/oats/issues/377)) ([3182f35](https://github.com/grafana/oats/commit/3182f357f4eb3d62a1474436c93bc334b6f52c20))
* **deps:** update opentelemetry-go monorepo to v1.44.0 ([#323](https://github.com/grafana/oats/issues/323)) ([098e2b2](https://github.com/grafana/oats/commit/098e2b247f0fd953f90637e7b7830c06e60f93d3))
* fall back when Compose engine is unreachable ([#428](https://github.com/grafana/oats/issues/428)) ([3d12ca6](https://github.com/grafana/oats/commit/3d12ca623d635b1aa0a661b7692cb017fbd7d061))
* **migrate:** preserve source line endings ([#432](https://github.com/grafana/oats/issues/432)) ([892d9f3](https://github.com/grafana/oats/commit/892d9f3d51ca08c1263c37dca5c6d1ceba649059))
* wait for lgtm deployment availability ([#337](https://github.com/grafana/oats/issues/337)) ([1f758f5](https://github.com/grafana/oats/commit/1f758f5d0639976cc584c89abbb76176e781b386))

## [0.7.0](https://github.com/grafana/oats/compare/v0.6.1...v0.7.0) (2026-05-28)


### Features

* automate immutable binary releases ([#314](https://github.com/grafana/oats/issues/314)) ([2eb602a](https://github.com/grafana/oats/commit/2eb602a72108b30166cebf631e74abed65e32e68))


### Bug Fixes

* **deps:** update module github.com/onsi/gomega to v1.40.0 ([#300](https://github.com/grafana/oats/issues/300)) ([c995bd6](https://github.com/grafana/oats/commit/c995bd61d9f5fd64a6dd5ec4d91afaeabd2e32cf))
* **deps:** update module github.com/onsi/gomega to v1.41.0 ([#319](https://github.com/grafana/oats/issues/319)) ([419de87](https://github.com/grafana/oats/commit/419de8753399d4de9befbd19c75270239f419dd2))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.52.0 ([#231](https://github.com/grafana/oats/issues/231)) ([f6bdcb8](https://github.com/grafana/oats/commit/f6bdcb8960ccece3e92b289f04d76ada3257c971))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.53.0 ([#240](https://github.com/grafana/oats/issues/240)) ([d335bd1](https://github.com/grafana/oats/commit/d335bd12d905fad140d5f5e78b2ceee26023b048))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.54.0 ([#257](https://github.com/grafana/oats/issues/257)) ([c8aa5c1](https://github.com/grafana/oats/commit/c8aa5c1a1ec57720fc376a998ed9623f81419234))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.55.0 ([#267](https://github.com/grafana/oats/issues/267)) ([05e6a0f](https://github.com/grafana/oats/commit/05e6a0f99f870162088a635388940ef5a7fc9cad))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.56.0 ([#276](https://github.com/grafana/oats/issues/276)) ([8f269bf](https://github.com/grafana/oats/commit/8f269bf2debf12b9109d277027cd0eb88d5f4546))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.57.0 ([#299](https://github.com/grafana/oats/issues/299)) ([f9f2434](https://github.com/grafana/oats/commit/f9f24345d1757d15cb95a777a877f939f071d81b))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.58.0 ([#312](https://github.com/grafana/oats/issues/312)) ([11df7da](https://github.com/grafana/oats/commit/11df7da667e57a2ed5ae82b629084aecc9a6f66e))
* **deps:** update module go.opentelemetry.io/collector/pdata to v1.59.0 ([#322](https://github.com/grafana/oats/issues/322)) ([af98e52](https://github.com/grafana/oats/commit/af98e52a4a691ad00004834f14d89544e767d284))
* **deps:** update opentelemetry-go monorepo to v1.41.0 ([#242](https://github.com/grafana/oats/issues/242)) ([518aefa](https://github.com/grafana/oats/commit/518aefaf1ecc1f8c1023ff0cb9f2930388d16745))
* **deps:** update opentelemetry-go monorepo to v1.42.0 ([#248](https://github.com/grafana/oats/issues/248)) ([694d5d9](https://github.com/grafana/oats/commit/694d5d9af683ad6f04d9b0efa6dbb160ac382c01))
* **deps:** update opentelemetry-go monorepo to v1.43.0 ([#271](https://github.com/grafana/oats/issues/271)) ([c29fad3](https://github.com/grafana/oats/commit/c29fad3c084603507bd9101382c0e04a88707737))
* reduce lychee retries to avoid compounding GitHub 429s ([#249](https://github.com/grafana/oats/issues/249)) ([6733e94](https://github.com/grafana/oats/commit/6733e94b14ce0a9573f91621f4ffc4b0be50185d))
* **security/unknown:** update module golang.org/x/net to v0.55.0 [security] ([#316](https://github.com/grafana/oats/issues/316)) ([94deefc](https://github.com/grafana/oats/commit/94deefcce189977402658df107d03002ef3e8a31))
* **security/unknown:** update module golang.org/x/sys to v0.44.0 [security] ([#317](https://github.com/grafana/oats/issues/317)) ([f7ee633](https://github.com/grafana/oats/commit/f7ee63389458bb44a044e9721967a4ef9fd56a96))

## Changelog

This file is managed by `release-please`.

For manual migration and breaking-change guidance, see [UPGRADING.md](UPGRADING.md).
