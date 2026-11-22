# Changelog

## [0.8.0](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.7.1...v0.8.0) (2026-11-21)


### ⚠ BREAKING CHANGES

* rename managed resources to cabpc-/cacpc- prefixes ([#113](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/113))

### Features

* rename managed resources to cabpc-/cacpc- prefixes ([#113](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/113)) ([36511db](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/36511db980f75618d9ca9d1faafc9505d4cd78b4))


### Miscellaneous

* backfill the 0.7 series in metadata.yaml ([#112](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/112)) ([0c2a165](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/0c2a1654fd2c8ce0960e216645362480ecd0cc3f))

## [0.7.1](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.7.0...v0.7.1) (2026-11-20)


### Features

* **controlplane:** aggregate a Ready condition and print Ready and Status columns ([#109](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/109)) ([bbfb03c](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/bbfb03cc07dd459cf669275ca0441c091ee4210d))
* **converge:** consume cargoship v0.43.1, exposing clusterConfig.tlsSan ([#110](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/110)) ([355d0fe](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/355d0fee1cc133d53566921f91ab055c1d7099c7))
* **converge:** match mirror credentials to registries proxying through it ([#107](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/107)) ([47c4cb3](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/47c4cb3c02b085746d97adba3eeb5c80e89f4eaf))


### Bug Fixes

* **controlplane:** skip the re-apply when the fingerprint matches but machines have not joined ([#106](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/106)) ([fd7d8c3](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/fd7d8c300467ff604f508d726d4eb014d9271045))
* **converge:** key host-key verification by Machine name instead of address ([#108](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/108)) ([f33881c](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/f33881ca317ab0f7158f3f33109a5cfe0158c2a6))

## [0.7.0](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.6.0...v0.7.0) (2026-11-20)


### ⚠ BREAKING CHANGES

* **controlplane:** resolve registry credentials from a dockerconfigjson secret ([#97](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/97))

### Features

* **controlplane:** resolve registry credentials from a dockerconfigjson secret ([#97](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/97)) ([9d616b2](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/9d616b2f4b03a2855ff60cf8402633ce55211a97))


### CI/CD

* list every past release series in metadata.yaml ([#98](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/98)) ([8d13d55](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/8d13d559329449ebeada1053248a449f5c5305e7))


### Build

* **deps:** bump actions/setup-go from 7.0.0 to 7.1.0 in the core group ([#105](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/105)) ([9057569](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/9057569ff6b98db611ded5b33cb49641a4e73de5))
* **deps:** bump github.com/go-openapi/validate from 1.0.1 to 1.0.2 ([#101](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/101)) ([1e1f02e](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/1e1f02ebfdda8a196e6f2e52076ac59701256f48))
* **deps:** bump github.com/grpc-ecosystem/grpc-gateway/v2 from 2.33.0 to 2.34.0 ([#102](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/102)) ([c530008](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/c5300088308c730363a80e51217051ac5bf8cd75))
* **deps:** bump github.com/sergi/go-diff from 1.4.0 to 1.4.5 ([#100](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/100)) ([466838f](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/466838f063822fbdac4a670314b57fa05e81e402))

## [0.6.0](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.5.5...v0.6.0) (2026-11-19)


### ⚠ BREAKING CHANGES

* **deps:** bump cargoship to 0.43.0 for .Cluster.LoadBalancer ([#92](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/92))

### Features

* **controlplane:** guard the cluster config and the fields the schema can reach ([#91](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/91)) ([0873148](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/0873148592de1c5cd4cb21a444c564bf76f728de))
* **controlplane:** verify package signatures against a public key secret ([#89](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/89)) ([a472009](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/a47200924ea3cdbff699d9149d0b14eb85548f2f))
* **converge:** assign control-plane profile to control-plane machines ([#96](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/96)) ([a2aedf1](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/a2aedf1196742446ad51e77bec8868b4729ecebe))
* **deps:** bump cargoship to 0.43.0 for .Cluster.LoadBalancer ([#92](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/92)) ([21456e7](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/21456e7af1a733712d6196fd79a5a541192b5cc2))


### Build

* **deps:** bump cloud.google.com/go/longrunning from 1.3.0 to 1.5.0 ([#94](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/94)) ([44777e3](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/44777e3b1d88db1039569a856a671b4397bf8e7f))
* **deps:** bump j178/prek-action from 3.1.0 to 3.1.1 in the core group ([#95](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/95)) ([8be3f1b](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/8be3f1bb028ae87436cb016ee26627745c5f2597))
* **deps:** bump the cluster-api group with 2 updates ([#93](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/93)) ([c0d8b91](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/c0d8b916f34a1bd1ad2345dc718e3dfcc3306cd4))

## [0.5.5](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.5.4...v0.5.5) (2026-11-18)


### CI/CD

* **release:** drop the redundant goreleaser dependency from the zarf publish job ([#87](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/87)) ([a14523c](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/a14523c770a980ea38109172df24d80b10d02c92))

## [0.5.4](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.5.3...v0.5.4) (2026-11-18)


### Bug Fixes

* populate host private address for firewall and mount ssh dir ([#84](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/84)) ([7e656bb](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/7e656bb29a87383f3474314f9aaa26fb4ac73856))

## [0.5.3](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.5.2...v0.5.3) (2026-11-18)


### CI/CD

* **release:** append goreleaser's notes so the cosign footer survives ([#81](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/81)) ([2839455](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/28394553614ce42649363e9a148b813d8b1785f1))


### Build

* **deps:** bump cargoship to 0.42.5 ([#83](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/83)) ([9f25062](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/9f250628c139c58a6b4cd6334526cc995b5b0685))

## [0.5.2](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.5.1...v0.5.2) (2026-11-18)


### Bug Fixes

* **controlplane:** log the underlying error when a converge fails ([#79](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/79)) ([1652e0e](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/1652e0ee0553567dca64eb96c3e9ff56f6b3a0fc))

## [0.5.1](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.5.0...v0.5.1) (2026-11-18)


### Miscellaneous

* **converge:** do not re-archive the distro package on load ([#78](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/78)) ([b0abc19](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/b0abc19251c42bf1dbcc98f032d02d23b70a1c59))


### Build

* **deps:** bump cargoship to 0.42.4 and drop the TMPDIR workaround ([#73](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/73)) ([8a3ecd0](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/8a3ecd0cb568eb31a2db4edb49a921800236060a))
* **deps:** bump github.com/sigstore/timestamp-authority/v2 from 2.2.0 to 2.2.1 ([#75](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/75)) ([0cc8bf5](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/0cc8bf5fb092274204ec3819590caf9d124521e7))

## [0.5.0](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.4.1...v0.5.0) (2026-11-17)


### ⚠ BREAKING CHANGES

* **config:** move the providers to cabpc-system and cacpc-system ([#70](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/70))

### Bug Fixes

* make a converge work against a real fleet from inside the management cluster ([#71](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/71)) ([6e66263](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/6e6626361efcbd5948f08ef95d2aea39483378ed))


### Refactoring

* **config:** move the providers to cabpc-system and cacpc-system ([#70](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/70)) ([7320a2c](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/7320a2c38978d4c2e4759d47515a0cd3750702cf))

## [0.4.1](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.4.0...v0.4.1) (2026-11-17)


### Miscellaneous

* **zarf:** make all components optional ([#68](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/68)) ([b37dbfd](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/b37dbfdb7c38e990b283818e5669821943544327))


### Build

* **deps:** bump github.com/docker/cli from 29.9.3+incompatible to 29.10.0+incompatible ([#67](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/67)) ([42b7dbf](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/42b7dbf413ae248d6932ab619de1d11edad11038))
* **deps:** bump https://github.com/google/keep-sorted from v0.10.0 to 0.11.0 ([#65](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/65)) ([0d90e89](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/0d90e89476860321aa8cd162bee4f6bb593cda33))

## [0.4.0](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.3.1...v0.4.0) (2026-11-17)


### ⚠ BREAKING CHANGES

* **api:** allow cross-namespace secret references ([#62](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/62))

### Features

* **api:** allow cross-namespace secret references ([#62](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/62)) ([5c744e8](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/5c744e8c4040778d46ead313f74ad8e3d6a0ea11))
* **zarf:** package pre-rendered Turtles ConfigMaps and CAPIProviders ([#60](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/60)) ([1b5ddc0](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/1b5ddc0dcb099bdea6b7d68ceca626bb45700504))


### Bug Fixes

* **controlplane:** clone the infrastructure template per machine ([#61](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/61)) ([cb356d3](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/cb356d3b72887ae7250de342a5ac3b174d2f00b4))

## [0.3.1](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.3.0...v0.3.1) (2026-11-16)


### Miscellaneous

* update zarf to include images in turtle component ([#59](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/59)) ([4b1e419](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/4b1e4194108b254a622bfbf97189dc65945d9f02))


### CI/CD

* generate the vendored osv-scanner overrides, and add a security policy ([#52](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/52)) ([a38fb25](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/a38fb257009ac44231f5524a3f0389dad71b811a))


### Build

* **deps:** bump github.com/agnivade/levenshtein from 1.2.1 to 1.2.2 ([#55](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/55)) ([cfb003a](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/cfb003aa6a7f794341cc61d1aa9b6eab70175a19))
* **deps:** bump github.com/googleapis/enterprise-certificate-proxy from 0.3.24 to 0.3.25 ([#56](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/56)) ([4ac45aa](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/4ac45aacaae0fde229ceebbf33dec660cf9215df))
* **deps:** bump github.com/prometheus/common from 0.73.1 to 0.74.0 ([#58](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/58)) ([f365fcd](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/f365fcd95fcc27a69d871c8026b5a7b7edd831c3))
* **deps:** bump github.com/sigstore/rekor from 1.6.0 to 1.6.1 ([#57](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/57)) ([f3fe6a2](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/f3fe6a22275c3fe4d434285c1a1f5f3b5c7d19ec))

## [0.3.0](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.2.3...v0.3.0) (2026-11-15)


### ⚠ BREAKING CHANGES

* build the zarf package after the artifact it pulls ([#51](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/51))

### Features

* **controlplane:** add CargoshipControlPlaneTemplate, the kind a ClusterClass names ([#44](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/44)) ([a981f00](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/a981f0099f56085b9ac1b30589bc3bd698f9f5c2))
* **examples:** publish a renderable ClusterClass for clusterctl to generate from ([#46](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/46)) ([e76d0bd](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/e76d0bdf05fc4cb783522fb257f8908228dd5e41))


### Bug Fixes

* **converge:** read a worker's profile from the class a topology generated it from ([#45](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/45)) ([ef5620b](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/ef5620bc081e85b68e20d88986b4958aa7e72320))


### Documentation

* add badges to the README ([#50](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/50)) ([5b10f48](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/5b10f48af6baeec31df2f64d1315c005fa5d26f6))
* record how the provider is driven by hand, and what zarf's webhook does to clusterctl ([#48](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/48)) ([756dcb0](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/756dcb08b012e35cdff22d3915894837dcbd1f72))


### CI/CD

* build the zarf package after the artifact it pulls ([#51](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/51)) ([5aec657](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/5aec657702ca267b82e676bb7d4ca47523b71656))

## [0.2.3](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.2.2...v0.2.3) (2026-11-15)


### Features

* **controlplane:** discover a distro package by name through the registry's records ([#39](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/39)) ([73d6cef](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/73d6cef9114de238f48ab0dd0e3d31c5338a1586))
* **controlplane:** read a zarf registry's credentials and CA from its own state ([#40](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/40)) ([fd6b656](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/fd6b656c47f8001dd7fadf639deb683cef52586d))
* **controlplane:** refuse an unapproved version change, and advertise the versions on offer ([#41](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/41)) ([226c664](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/226c664e0c2e2c6a4edcfb3dec9f2e75635e780a))


### Build

* **deps:** bump cargoship to 0.42.2 for the mirror record API ([#38](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/38)) ([8c515b8](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/8c515b899295b6d52b131c2ccb5466d11f29ee49))

## [0.2.2](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.2.1...v0.2.2) (2026-11-14)


### CI/CD

* publish the air-gapped artifacts from the release workflow rather than the release event ([#36](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/36)) ([e92362e](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/e92362e208ad5802c9e2132e16920cca63c23196))

## [0.2.1](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.2.0...v0.2.1) (2026-11-14)


### CI/CD

* publish the operator artifact and the zarf package on a release ([#34](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/34)) ([94f040a](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/94f040a9ea0ed04594229511c14dd49b05766f5f))

## [0.2.0](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.1.3...v0.2.0) (2026-11-14)


### ⚠ BREAKING CHANGES

* drive both reconcilers against a real api server ([#26](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/26))

### Documentation

* add a guide for wiring rancher turtles to this provider ([#10](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/10)) ([e449e62](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/e449e62c42de406cbe93e15391551322c4d28fd2))


### Tests

* drive both reconcilers against a real api server ([#26](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/26)) ([475a4b3](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/475a4b33fe3f63659ffbeb0a6f9d4eb903d85599))


### Build

* add a zarf v1beta1 package for air-gapped installs ([#9](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/9)) ([0dc8633](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/0dc8633c13cc04f7553fdcc4487bb526b179a088))
* **deps:** bump controller-tools to 0.23.0, watch the kustomize hold ([#32](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/32)) ([e71a3fd](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/e71a3fd088ca50ac3d5ab52f3c2ea26473aea3f8))

## [0.1.3](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.1.2...v0.1.3) (2026-11-14)


### Documentation

* add the book, guides and decision records ([#7](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/7)) ([5979ae6](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/5979ae6c86582b50421987a9d6d7e642a9638596))


### CI/CD

* fix wrong signing secrets ([#30](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/30)) ([b8e56e0](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/b8e56e0a67c891db7c4063339a0ce4d3fb73e56d))

## [0.1.2](https://github.com/colonel-byte/cluster-api-provider-cargoship/compare/v0.1.1...v0.1.2) (2026-11-14)


### Bug Fixes

* **ci:** sign release artifacts with cosign bundles and attest the AI-BOM ([#28](https://github.com/colonel-byte/cluster-api-provider-cargoship/issues/28)) ([24964d0](https://github.com/colonel-byte/cluster-api-provider-cargoship/commit/24964d097c2c858086dece302cb8c0229cc08ab1))

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
