# Changelog

## [0.2.0](https://github.com/Superintelligent-Group/superintelligent-e2b/compare/docker-reverse-proxy-v0.1.0...docker-reverse-proxy-v0.2.0) (2026-08-21)


### Features

* **api:** soft-delete build layers in DB on user delete ([#3121](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3121)) ([ee88776](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/ee887768cf28e657f36aa7038495fad37859c144))
* **docker-reverse-proxy:** gate access token auth behind feature flag ([#3242](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3242)) ([19db283](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/19db2830c9bf8f7fe6a8aeaee9588ad49578b61d))


### Bug Fixes

* **aws:** bind write destinations to account authority ([4486257](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/44862571c33852adae892f2d45b091573becc703))
* **aws:** gate Make inputs before expansion ([5f2e31a](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/5f2e31ae18bb25bfdf3aa99c9097fd930cb3a68c))
* **aws:** reject untrusted Make destinations ([e0f1abb](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/e0f1abb867dafed54545d32bc29e616f806f1cd7))
* correct 3 CVES ([#3218](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3218)) ([076823b](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/076823bc5cbffb9f8c04670c886562445e50ead7))
* forgot to add docker- to the docker repository ([#3186](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3186)) ([b04225b](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/b04225ba2e89881029ef8ddc9d2d168d250ae90b))
* **iac:** guard every AWS artifact write ([03207db](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/03207db17c31ce27817dba3104bdf7e61ae489ab))
* push client-proxy, dashboard-api, and docker-reverse-proxy image… ([#2953](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/2953)) ([1d930ee](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/1d930ee60fd74b3ad1d5c167165b1005baa471fe))
* reset artifacts ([#3259](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3259)) ([93f7eb5](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/93f7eb57ce66fb72607bc0f3c1c40358a3c46c8a))
* update go to 1.26.4 for CVE-2026-42504 ([#3188](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3188)) ([a5cc1ff](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/a5cc1ff0726f18edb505b4b776f0f40756323e25))

## [0.1.0](https://github.com/Superintelligent-Group/superintelligent-e2b/compare/docker-reverse-proxy-v0.0.1...docker-reverse-proxy-v0.1.0) (2026-08-09)


### Features

* **api:** soft-delete build layers in DB on user delete ([#3121](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3121)) ([ee88776](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/ee887768cf28e657f36aa7038495fad37859c144))
* **docker-reverse-proxy:** gate access token auth behind feature flag ([#3242](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3242)) ([19db283](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/19db2830c9bf8f7fe6a8aeaee9588ad49578b61d))


### Bug Fixes

* **aws:** bind write destinations to account authority ([4486257](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/44862571c33852adae892f2d45b091573becc703))
* **aws:** gate Make inputs before expansion ([5f2e31a](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/5f2e31ae18bb25bfdf3aa99c9097fd930cb3a68c))
* **aws:** reject untrusted Make destinations ([e0f1abb](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/e0f1abb867dafed54545d32bc29e616f806f1cd7))
* correct 3 CVES ([#3218](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3218)) ([076823b](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/076823bc5cbffb9f8c04670c886562445e50ead7))
* forgot to add docker- to the docker repository ([#3186](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3186)) ([b04225b](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/b04225ba2e89881029ef8ddc9d2d168d250ae90b))
* **iac:** guard every AWS artifact write ([03207db](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/03207db17c31ce27817dba3104bdf7e61ae489ab))
* push client-proxy, dashboard-api, and docker-reverse-proxy image… ([#2953](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/2953)) ([1d930ee](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/1d930ee60fd74b3ad1d5c167165b1005baa471fe))
* reset artifacts ([#3259](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3259)) ([93f7eb5](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/93f7eb57ce66fb72607bc0f3c1c40358a3c46c8a))
* update go to 1.26.4 for CVE-2026-42504 ([#3188](https://github.com/Superintelligent-Group/superintelligent-e2b/issues/3188)) ([a5cc1ff](https://github.com/Superintelligent-Group/superintelligent-e2b/commit/a5cc1ff0726f18edb505b4b776f0f40756323e25))

## 0.0.1 (2026-07-10)


### Features

* **api:** soft-delete build layers in DB on user delete ([#3121](https://github.com/e2b-dev/infra/issues/3121)) ([ee88776](https://github.com/e2b-dev/infra/commit/ee887768cf28e657f36aa7038495fad37859c144))
* **docker-reverse-proxy:** gate access token auth behind feature flag ([#3242](https://github.com/e2b-dev/infra/issues/3242)) ([19db283](https://github.com/e2b-dev/infra/commit/19db2830c9bf8f7fe6a8aeaee9588ad49578b61d))


### Bug Fixes

* correct 3 CVES ([#3218](https://github.com/e2b-dev/infra/issues/3218)) ([076823b](https://github.com/e2b-dev/infra/commit/076823bc5cbffb9f8c04670c886562445e50ead7))
* forgot to add docker- to the docker repository ([#3186](https://github.com/e2b-dev/infra/issues/3186)) ([b04225b](https://github.com/e2b-dev/infra/commit/b04225ba2e89881029ef8ddc9d2d168d250ae90b))
* push client-proxy, dashboard-api, and docker-reverse-proxy image… ([#2953](https://github.com/e2b-dev/infra/issues/2953)) ([1d930ee](https://github.com/e2b-dev/infra/commit/1d930ee60fd74b3ad1d5c167165b1005baa471fe))
* reset artifacts ([#3259](https://github.com/e2b-dev/infra/issues/3259)) ([93f7eb5](https://github.com/e2b-dev/infra/commit/93f7eb57ce66fb72607bc0f3c1c40358a3c46c8a))
* update go to 1.26.4 for CVE-2026-42504 ([#3188](https://github.com/e2b-dev/infra/issues/3188)) ([a5cc1ff](https://github.com/e2b-dev/infra/commit/a5cc1ff0726f18edb505b4b776f0f40756323e25))
