# Changelog

## [0.1.1](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.1.0...v0.1.1) (2026-11-14)


### Features

* **api:** add the bootstrap and control-plane CRDs ([#2](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/2)) ([08e2888](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/08e288824aa5a0cd87e9f9c054d9de26b5349179))
* **bootstrap:** render bootstrap data that only enables ssh ([#4](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/4)) ([4ed470d](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/4ed470d637adf00f35e3208478580438a08efc15))
* **controlplane:** converge the fleet without timers ([#5](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/5)) ([74e4c0e](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/74e4c0ede19aaccf72d65f945c7f9f17255fdfc4))
* **converge:** build a cargoship inventory from Machine objects ([#3](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/3)) ([29c21db](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/29c21dbe2f1f1bb3c74fa30a80e7f715f77d4f84))
* **init:** repo creation ([089dc46](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/089dc4658ea813b00e6f3491c4d34ce0fd2429e2))


### CI/CD

* add the release pipeline and component manifests ([#6](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/6)) ([be2a5fe](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/be2a5fea6a0da26842b47936d3717cd5493dbb3a))


### Build

* **deps:** bump github.com/andybalholm/brotli from 1.2.8 to 1.3.0 ([#23](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/23)) ([f4bef1a](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/f4bef1abd81eb5c08c95a7c0a52ff83ffe82e4c8))
* **deps:** bump github.com/davidmz/go-pageant from 1.0.2 to 1.1.0 ([#19](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/19)) ([991f4dd](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/991f4dd283aa7698f5a86a61d27739ac81f9766f))
* **deps:** bump github.com/docker/docker-credential-helpers from 0.9.12 to 0.9.13 ([#25](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/25)) ([90fe7ea](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/90fe7ea4d601a3184c53da94ce46ed6942a03f24))
* **deps:** bump github.com/onsi/ginkgo/v2 from 2.32.1 to 2.34.0 ([#18](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/18)) ([61a4bdf](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/61a4bdf12c58beae0cc6a83536558bbf583fa0c5))
* **deps:** bump github.com/onsi/gomega from 1.42.1 to 1.44.0 ([#24](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/24)) ([579a3f8](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/579a3f8fe58d03cb3fc11304e02c69f9b5c696d6))
* **deps:** bump github.com/prometheus/procfs from 0.22.0 to 0.23.0 ([#15](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/15)) ([c0367ce](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/c0367ce8eb03fcc0fba5ef97535f2ef67bde22a9))
* **deps:** bump github.com/sigstore/rekor from 1.5.5 to 1.6.0 ([#22](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/22)) ([44ab2a8](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/44ab2a89e46d5eedd84fdc0864786af1ecd6ea29))
* **deps:** bump github.com/sigstore/rekor-tiles/v2 from 2.4.1 to 2.4.2 ([#16](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/16)) ([2ad15d4](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/2ad15d4fa241248ee6b4cd881f1d72ddcc090c2b))
* **deps:** bump github.com/zarf-dev/zarf from 0.89.0 to 1.0.0 ([#21](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/21)) ([6b6a50f](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/6b6a50fae595a31880c90f12bd5d4528cbce133b))
* **deps:** bump golang.org/x/crypto from 0.58.0 to 0.59.0 in the golang group ([#14](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/14)) ([3f25a2c](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/3f25a2ca1f2b9af53e079872f0c4d850bfc77df8))
* **deps:** bump helm/kind-action from 1.12.0 to 1.15.2 in the core group ([#17](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/17)) ([e9c823f](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/e9c823f442ed62fb8204e5e61c80a2e2a0add03f))
* **deps:** bump the k8s group with 2 updates ([#13](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/13)) ([bb17018](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/bb170183d73f21c0c9d262076434d4f89b30869d))
* **deps:** vendor the cluster-api and controller-runtime tree ([#1](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/1)) ([bea0476](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/bea047623d0dd72876a08adf2a3fddfa43088be8))
