# Changelog

## [0.228.1](https://github.com/sergiught/auth0-mock/compare/v0.228.0...v0.228.1) (2026-05-24)


### Bug fixes

* **deps:** bump golang.org/x/sys to v0.45.0 ([#24](https://github.com/sergiught/auth0-mock/issues/24)) ([ae61ec9](https://github.com/sergiught/auth0-mock/commit/ae61ec92bc6d4c357583ae955c1dcfefc7fc1629))

## [0.228.0](https://github.com/sergiught/auth0-mock/compare/v0.227.0...v0.228.0) (2026-05-19)


### Features

* **events:** real SSE for GET /api/v2/events ([#16](https://github.com/sergiught/auth0-mock/issues/16)) ([7ec8365](https://github.com/sergiught/auth0-mock/commit/7ec83650f37fb04660924fecf9e64ba2fc3b1057))

## [0.227.0](https://github.com/sergiught/auth0-mock/compare/v0.226.0...v0.227.0) (2026-05-17)


### Features

* runtime clock control for deterministic token-expiry tests ([#12](https://github.com/sergiught/auth0-mock/issues/12)) ([baa78e6](https://github.com/sergiught/auth0-mock/commit/baa78e6a6c0427f23cd0bb2425af75663a1d7f9b))

## [0.226.0](https://github.com/sergiught/auth0-mock/compare/v0.225.0...v0.226.0) (2026-05-17)


### Features

* **sdk:** pkg/auth0mock — typed Go client for the auth0-mock control plane ([#10](https://github.com/sergiught/auth0-mock/issues/10)) ([93084c8](https://github.com/sergiught/auth0-mock/commit/93084c85910854b55de480a196c298f3114c172f))
* **server:** per-expectation IDs, hit counters, header matching, per-stub endpoints ([#9](https://github.com/sergiught/auth0-mock/issues/9)) ([2c6f725](https://github.com/sergiught/auth0-mock/commit/2c6f725aec5cc5fb99cebda20857d73f8a37dd71))

## 0.225.0 (2026-05-16)


### Features

* **admin0:** /admin0/reset and /admin0/matches ([83f0408](https://github.com/sergiught/auth0-mock/commit/83f0408fb651dc8d995dc40fa3ada5cc593dfb89))
* **admin0:** nested request+response expectation payload ([b739361](https://github.com/sergiught/auth0-mock/commit/b739361e179bf3d4fa725d461fc917439b8bfb4d))
* **api:** embed Auth0 Management API OpenAPI 3.1 spec ([dfd8691](https://github.com/sergiught/auth0-mock/commit/dfd86911aebfd13af419a803f6b1e86e7cbcbd89))
* **authapi,admin0:** MFA challenge flow with canned factors ([e91a3af](https://github.com/sergiught/auth0-mock/commit/e91a3afbc900a2f35d5aab63cc985303f2edae78))
* **authapi,admin0:** runtime custom claims + per-audience permissions ([27acc83](https://github.com/sergiught/auth0-mock/commit/27acc833cf97c8986ac545b7f359426c7aa29f66))
* **authapi:** /.well-known/openid-configuration ([a900489](https://github.com/sergiught/auth0-mock/commit/a9004894e538899c3fe278c14d35d16673a7fa27))
* **authapi:** /authorize, /userinfo, /v2/logout, /oauth/revoke ([b55de0f](https://github.com/sergiught/auth0-mock/commit/b55de0f02edba444ac6ec23b6d25fb5fdc3da10d))
* **authapi:** /dbconnections/signup and /dbconnections/change_password ([b6241fd](https://github.com/sergiught/auth0-mock/commit/b6241fd2376c5cf69275a8ea07603ba6d12b0d43))
* **authapi:** /oauth/token for all four grant types ([dd59fbb](https://github.com/sergiught/auth0-mock/commit/dd59fbb210dbcce3243fbe4c00cd283d692370b4))
* **authapi:** enforce PKCE on authorization_code grant ([7278a4b](https://github.com/sergiught/auth0-mock/commit/7278a4b5c8e2785daa8be266100295759217cd5f))
* **authapi:** mount skeleton + request/response types ([3b267b1](https://github.com/sergiught/auth0-mock/commit/3b267b19220818c19ca8893500214c30c9502f6a))
* **authapi:** password-realm grant for Auth0 Native SDKs ([06e3a76](https://github.com/sergiught/auth0-mock/commit/06e3a76088793c5df457a27f4b7229ae5802da01))
* **authapi:** passwordless start + verify (fixed OTP 000000) ([62201e7](https://github.com/sergiught/auth0-mock/commit/62201e71ed34acb74a9d3ac2643554545e284a29))
* **bearer:** bearer-token middleware over jwks ([3e2d532](https://github.com/sergiught/auth0-mock/commit/3e2d532c7215d233dcf97e5034f16ebd34369cff))
* cmd/api entrypoint wires config, logger, store, server ([6d8587b](https://github.com/sergiught/auth0-mock/commit/6d8587bf4f2163dae1613fc6fcdc559e3eb54db4))
* **config:** default HTTP/HTTPS bind to 127.0.0.1 ([fb03f63](https://github.com/sergiught/auth0-mock/commit/fb03f63d2e0b9ee881e1d7a7f7c4625d8521c069))
* **config:** load runtime settings from env ([b59ee27](https://github.com/sergiught/auth0-mock/commit/b59ee27c92b3a3419e53b0a2c856aff1ef9cc355))
* **docs:** add custom header bar to the API reference page ([1ff68bc](https://github.com/sergiught/auth0-mock/commit/1ff68bc6236d1f063576db581eddbd489cb76d6a))
* **docs:** apply the custom Scalar theming layer ([c0de09f](https://github.com/sergiught/auth0-mock/commit/c0de09f97ec9e1df1dac6f32a14c4de59d16add3))
* **docs:** drive light/dark from the header toggle ([a2e8415](https://github.com/sergiught/auth0-mock/commit/a2e84158c3bdff2509f6a7675821fbb5c73a1bea))
* **docs:** self-host Geist fonts and serve the docs stylesheet ([b8659ea](https://github.com/sergiught/auth0-mock/commit/b8659ea9894179831337b619538c8f44bae31f57))
* **examples:** drive the consumer demo with the real go-auth0 SDK ([82b656b](https://github.com/sergiught/auth0-mock/commit/82b656b392fdda1e33cd0af89e7db7543a01496d))
* **examples:** validate TLS in the consumer demo and polish the walkthrough ([cdf3eb3](https://github.com/sergiught/auth0-mock/commit/cdf3eb3cc26e602ed0c6c266bdb52ea1b8b4193e))
* **expectations:** add centralized /admin0/expectations endpoints ([1b80636](https://github.com/sergiught/auth0-mock/commit/1b806367bde84d1abb3c67929d4f189338aa5499))
* **expectations:** add matches.KindOf and spec.Validator.Resolve ([b09eaad](https://github.com/sergiught/auth0-mock/commit/b09eaadb73e9f81cb51f447e134a2714bf3ffabb))
* **expectations:** describe /admin0/expectations in the merged spec ([9895812](https://github.com/sergiught/auth0-mock/commit/98958127745b370e17f0db5b4e1afba0b3135ee4))
* **httperr:** JSON error writer for mgmt + auth shapes ([60a6ce3](https://github.com/sergiught/auth0-mock/commit/60a6ce3b48bc2aef0588fc267ab9d1e992efc87f))
* **install:** add install.sh — curl|bash with version pinning + sha256 verify ([5960158](https://github.com/sergiught/auth0-mock/commit/5960158ed7cc8b802a401194122157dc66e885b6))
* **jwks:** mint and verify RS256 tokens ([531638e](https://github.com/sergiught/auth0-mock/commit/531638edb32ff009d3ba297cfe41c936eb15b9cb))
* **jwks:** publish JWKS JSON document ([3faba6e](https://github.com/sergiught/auth0-mock/commit/3faba6eb28c25e8bc37d12b46a32b0658954ccbb))
* **jwks:** RS256 KeySet with generate-or-load ([499941f](https://github.com/sergiught/auth0-mock/commit/499941fdae0191931a5e9a5d296dc9edaff2f807))
* **logger:** construct zerolog logger per environment ([1d900ec](https://github.com/sergiught/auth0-mock/commit/1d900ec98eece3e3977576043d924c2271b5160b))
* **logger:** full-word level labels, compact timestamp, single eye-catching glyph on warn/error ([3e37f3d](https://github.com/sergiught/auth0-mock/commit/3e37f3d9f3279b20481412d6a4cc5a9e7a5014f5))
* **matches:** add List, ResetEndpoint, ResetAll ([e814a6c](https://github.com/sergiught/auth0-mock/commit/e814a6c7c2235fc64dd84c285888f81eaf5038ce))
* **matches:** define Match and Kind types ([d9edc85](https://github.com/sergiught/auth0-mock/commit/d9edc859a5ce37f690e25b69117e5d94d67af082))
* **matches:** in-memory store with Put and Find ([3417d7a](https://github.com/sergiught/auth0-mock/commit/3417d7a8218df69eeb6c530a15a61acac522c8db))
* **mgmtapi:** generic handler validates, finds match, writes ([0786c32](https://github.com/sergiught/auth0-mock/commit/0786c328625e45c5ff6551269c28479c41ba06de))
* **mgmtapi:** match expectations on the incoming request ([9b7bd6f](https://github.com/sergiught/auth0-mock/commit/9b7bd6f795118169318d677fd3ef219b24866ca3))
* **mgmtapi:** match handler validates and stores registrations ([d620e05](https://github.com/sergiught/auth0-mock/commit/d620e0537cbfdc9edbca71f77d6b065267fedd86))
* **mgmtapi:** Mount registers original + match + reset routes ([43ff545](https://github.com/sergiught/auth0-mock/commit/43ff54542a74f37bdcef2199f032bd8888801e07))
* **mgmtapi:** OpenAPI↔httprouter path translation ([e7685da](https://github.com/sergiught/auth0-mock/commit/e7685dae47d13531a0211f66d4442c7b502a7842))
* **mgmtapi:** reset handler clears registered scope ([ee710bb](https://github.com/sergiught/auth0-mock/commit/ee710bb49ed571c713589e928b34af5fef8ae9ad))
* **middleware+logger:** action-first log lines, filtered headers, blank-line separator, panic stack pretty-printed ([019500a](https://github.com/sergiught/auth0-mock/commit/019500a23021a703a7b0170e7920703ee0ae5ae8))
* **middleware:** DEBUG=true dumps every request + response with bearer/secret redaction ([192c15f](https://github.com/sergiught/auth0-mock/commit/192c15fa8dc2fbf67b6cd6f9f50ebc9b6e6bbf24))
* **middleware:** print request + response bodies as indented blocks (pretty JSON, no escape-soup) ([480398d](https://github.com/sergiught/auth0-mock/commit/480398d5bbde08bb29d1e6ca6b5db08703abfece))
* **middleware:** recovery, request_id, logging ([e13efa0](https://github.com/sergiught/auth0-mock/commit/e13efa021b9718df87aeffad26af9c24da272295))
* **openapi:** add shared mock-control schemas for match/reset ([f6705a2](https://github.com/sergiught/auth0-mock/commit/f6705a2647c65a79969cbaf04d8979de72eb0aac))
* **openapi:** describe admin0 control plane in fragment ([bac697b](https://github.com/sergiught/auth0-mock/commit/bac697b03a4d1358f8948af98ea2be5ce7f16274))
* **openapi:** describe auth-api endpoints in authapi fragment ([9afd08c](https://github.com/sergiught/auth0-mock/commit/9afd08cf6783f3cf5dc0b19788dfb53dde98e0a5))
* **openapi:** describe service endpoints in router fragment ([ed3c064](https://github.com/sergiught/auth0-mock/commit/ed3c06456667f3e0b8f8564bc8088f9b9691e9cd))
* **openapi:** generate and embed merged auth0-mock spec ([3e7a2ec](https://github.com/sergiught/auth0-mock/commit/3e7a2ec06c5bdc9b88ffb10c6144f3e2b7fa5c7a))
* **openapi:** group sidebar via x-tagGroups and inline siblings ([884c29d](https://github.com/sergiught/auth0-mock/commit/884c29d255347e09a32868e2ae814b080d6d1583))
* **openapi:** label match/reset siblings with the parent's Auth0 summary ([68c7bf0](https://github.com/sergiught/auth0-mock/commit/68c7bf0e81daeb2caf159aaf5009d7323c903f16))
* **openapi:** merge fragments with conflict detection ([f1fe22f](https://github.com/sergiught/auth0-mock/commit/f1fe22f0da31eae2d252e861700420ce8c390c79))
* **openapi:** preload bearer + follow OS theme on /docs ([a5829aa](https://github.com/sergiught/auth0-mock/commit/a5829aa6982fe7b11d65fd8f7a48f4dff3cb28e3))
* **openapi:** rebrand merged spec info as auth0-mock ([d0ff4f5](https://github.com/sergiught/auth0-mock/commit/d0ff4f58063412504e78b57d3d72dd2c18124609))
* **openapi:** serve merged spec at /openapi.json and /openapi.yaml ([30b777b](https://github.com/sergiught/auth0-mock/commit/30b777ba5a737e7b0215833b775c57dd2a7b6883))
* **openapi:** serve scalar-rendered API reference at /docs ([523559f](https://github.com/sergiught/auth0-mock/commit/523559f274ab755bd1c60f8bd292e188c9bfa3e0))
* **openapi:** split surface tags so docs sidebar nesting is meaningful ([2d9313d](https://github.com/sergiught/auth0-mock/commit/2d9313d18da7789d5767c512a11762708f956580))
* **openapi:** synthesise per-operation match and reset siblings ([4a1496d](https://github.com/sergiught/auth0-mock/commit/4a1496da0f0a6b396398314f29be7559a9851fe4))
* **openapi:** vendor a stripped Auth0 API skeleton, not the full spec ([eced22a](https://github.com/sergiught/auth0-mock/commit/eced22a46c0cb2160f6e6b2998d9b5061cb77de7))
* **ops:** add /readyz, multi-Go CI matrix, godog junit report, move vuln to pre-push ([1d9f335](https://github.com/sergiught/auth0-mock/commit/1d9f335726a629b5ab445991e3d9af1e8d9f0176))
* **router:** add /healthz liveness endpoint ([022d8aa](https://github.com/sergiught/auth0-mock/commit/022d8aa6aeda9625f01b6397fd3355b10f8a5416))
* **router:** wire admin0 and middleware chain ([9c289ca](https://github.com/sergiught/auth0-mock/commit/9c289ca14e74cfff8ed09f6e145c45f8fa3e283d))
* **server:** harden boot — timeouts, body cap, graceful validator init ([b190b08](https://github.com/sergiught/auth0-mock/commit/b190b0867ebc97ae7b9de602c2e2ebc45e5542b7))
* **server:** HTTP server + orchestrator with graceful shutdown ([99ab0a1](https://github.com/sergiught/auth0-mock/commit/99ab0a133450bad9a7bc1654674a28ec27229a00))
* **spec:** iterator over all operations in the spec ([241ce14](https://github.com/sergiught/auth0-mock/commit/241ce14b46bcc01739ae95a662db9710648be951))
* **spec:** load and validate Auth0 OpenAPI 3.1 spec ([ea1f4b7](https://github.com/sergiught/auth0-mock/commit/ea1f4b7130937ac5f1f15c52910b05801590dd28))
* **spec:** request, response, registration validators ([3ea3578](https://github.com/sergiught/auth0-mock/commit/3ea357850df6c9737920e78ed059f5f3da98bf65))
* **spec:** validate request-matcher body and query at registration ([68d3ebc](https://github.com/sergiught/auth0-mock/commit/68d3ebc63cbb731ed6c186262b4ec48aa46d1868))
* **tlscert:** persist auto-generated cert via TLS_CACHE_DIR + mkcert docs ([13bbd53](https://github.com/sergiught/auth0-mock/commit/13bbd5351e2c545f2035a8b4c02a3c299f40adb6))
* **tlscert:** self-signed cert generator + file loader ([8d925bb](https://github.com/sergiught/auth0-mock/commit/8d925bbd83881e92a016034de56cc24d2e8fbd22))
* **version:** expose Version/Commit/Date via -ldflags + -version flag ([c293b05](https://github.com/sergiught/auth0-mock/commit/c293b05a93ed1ca0d91785de273f1cb3561f934f))
* wire Auth API into router and main ([221841e](https://github.com/sergiught/auth0-mock/commit/221841e00af6cf9b2e7ca769dd004914ae12edb5))
* wire JWKS endpoint and HTTPS server into main ([1ed04a5](https://github.com/sergiught/auth0-mock/commit/1ed04a5d2143e78b657d9904bcf7c2ebc5275d00))
* wire mgmtapi mount into router and main ([f407dd6](https://github.com/sergiught/auth0-mock/commit/f407dd60b696f0badcc90e0d216855ca3d9d5de3))


### Bug fixes

* **authapi:** /passwordless/start returns email in the email field, not the connection name ([c7b1888](https://github.com/sergiught/auth0-mock/commit/c7b18881fb740041de7f7b449f84dccbffc64b63))
* **authapi+docs:** logout default-permissive matches authorize; readyz docs match reality ([b755551](https://github.com/sergiught/auth0-mock/commit/b755551b9c5213b617fe5fb464cfc4d66aeff0d3))
* **authapi:** close dangerous-scheme + backslash bypass on /v2/logout; add allow-list to /authorize ([2c83ab7](https://github.com/sergiught/auth0-mock/commit/2c83ab73d1cdcf28050899f87d1bd9ee4dbc993e))
* **authapi:** close every multi-slash + whitespace bypass on opted-in allow-list; tidy attribution + makefile ([297460e](https://github.com/sergiught/auth0-mock/commit/297460e2a00c7816dd4b5422eb061d08cb0dfa13))
* **authapi:** close open-redirect on /v2/logout + close PKCE bypass ([9dceb17](https://github.com/sergiught/auth0-mock/commit/9dceb17faeea72b64e13eb0bdbb16d9ccbbc4af7))
* **ci:** hoist DOCKERHUB_USERNAME to job env so release.yml can register ([3c4968c](https://github.com/sergiught/auth0-mock/commit/3c4968c50c6370435195bfc9fa9f0a2f5b3b7c3a))
* **contract:** wire LOG_LEVEL, make HTTPS_ADDR=off honest, relax header-max ([ac72c34](https://github.com/sergiught/auth0-mock/commit/ac72c342d5d3e5140d8defbca8cc3bcead776062))
* **demo:** three-tier port-busy precheck (ss → lsof → nc) + surface make demo in README ([0bb73ae](https://github.com/sergiught/auth0-mock/commit/0bb73ae33758bd33dffc667db52d5aa9e8f53bfa))
* **docs:** drive the theme toggle off &lt;body&gt;, not <html> ([c7d30d3](https://github.com/sergiught/auth0-mock/commit/c7d30d3a5fee1d12c3023153be452df552dbceee))
* **expectations:** drop stale /admin0/matches test and probe URL ([190ef89](https://github.com/sergiught/auth0-mock/commit/190ef898a891f9517696af52bf2aff0ca2ade6a2))
* **expectations:** migrate consumer example, canonicalise template keys ([7861777](https://github.com/sergiught/auth0-mock/commit/7861777660fcae3d803f8fd494c3bf49cec0a977))
* fourth-pass cleanup — PKCE length, store-growth caveat, terminology, install hint ([df4ac59](https://github.com/sergiught/auth0-mock/commit/df4ac59610ae1308a905e7dcfba8ec8e961c869f))
* **genopenapi:** strip Auth0 prose from Info + Servers blocks too ([ddfad19](https://github.com/sergiught/auth0-mock/commit/ddfad1956eeb79b8e678f22f35119c4669bb8001))
* **genopenapi:** strip x-* extensions from ref wrappers, Discriminator, OAuth flows ([c5b579c](https://github.com/sergiught/auth0-mock/commit/c5b579ca62e4f95fc531295c87710f46fd9f49c2))
* **jwks:** harden verify (leeway, iat) + opt-in audience binding + drop KeySet.PrivateKey ([9eb9368](https://github.com/sergiught/auth0-mock/commit/9eb9368e9b2d27bf41d7b1c99d8fe373a467b0c7))
* **lint:** restore G115 nolint + allow-unused nolintlint so cache-stale doesn't strip suppressions ([6c953a8](https://github.com/sergiught/auth0-mock/commit/6c953a89110cd5a8c2c0f193bfdfa2f195c70ae3))
* **mgmtapi:** trim verbose kin-openapi validation errors to one actionable line ([6e81401](https://github.com/sergiught/auth0-mock/commit/6e81401e3e8e24a4d821c2fbd3d77aefe36a3172))
* **middleware,mgmtapi:** suppress dump noise and shape /api/v2 404+405 as JSON ([306c4a2](https://github.com/sergiught/auth0-mock/commit/306c4a2c9cf1ae431065366d863ef83524188564))
* **middleware:** expand noisy-header denylist to catch browser fingerprinting (Sec-*, Dnt, Priority, …) ([052281a](https://github.com/sergiught/auth0-mock/commit/052281ad12d452479b00f56964c16df830a4ca9c))
* **middleware:** preserve MaxBytesError under DEBUG; redact-before-truncate so cross-cap tokens can't leak ([e128d88](https://github.com/sergiught/auth0-mock/commit/e128d888a54de7b53000819348b014d7a414c3f1))
* **openapi:** align auth-api fragment with handler behaviour ([253f9bf](https://github.com/sergiught/auth0-mock/commit/253f9bffcd9a5967658479318f30b5cb0a9417e9))
* **openapi:** blank stub run params to silence unusedparams ([6d9ccb1](https://github.com/sergiught/auth0-mock/commit/6d9ccb1a73813a74bc079629a07b4626f366e57e))
* **openapi:** disable scalar agent so /docs doesn't upload the spec ([30415ae](https://github.com/sergiught/auth0-mock/commit/30415ae3248cfce2d195d957feef8b5a5c383d33))
* **openapi:** disable scalar navbar developer tools on /docs ([29a64df](https://github.com/sergiught/auth0-mock/commit/29a64df52e29fd6976b7f52ab97c1c6950cc659c))
* **openapi:** give match/reset siblings unique ids and summaries ([2a02e55](https://github.com/sergiught/auth0-mock/commit/2a02e55482f4023318d0ca107806c31de8bef7c0))
* **openapi:** pin and SRI-guard the scalar bundle on /docs ([2bbbc0d](https://github.com/sergiught/auth0-mock/commit/2bbbc0d2bb31d418dce4d0c458d2805d98b26acc))
* **openapi:** prefix mgmt-api paths with /api/v2 in merged doc ([1a2cac4](https://github.com/sergiught/auth0-mock/commit/1a2cac40a3fb77ac74325d072b6d0b964e7b1a70))
* **openapi:** repoint externalDocs and reload spec on regen ([901b684](https://github.com/sergiught/auth0-mock/commit/901b68404baba40a817efed79f57897589c125f4))
* **openapi:** synthesise match/reset siblings per parent method ([6e2f46e](https://github.com/sergiught/auth0-mock/commit/6e2f46e18071176569dc6e641493f5ed625877c8))
* **ops:** make demo bails on port conflict; release.yml gates dockerhub steps on creds ([d047e02](https://github.com/sergiught/auth0-mock/commit/d047e02e73ad28d704e9cfc01d3602de629169fe))
* pre-tag audit gaps — goreleaser format key, README env vars, CHANGELOG terminology ([cdeb351](https://github.com/sergiught/auth0-mock/commit/cdeb35176bd00f2e252bffa78120a9eea63b75b7))
* **release+commitlint:** unbreak goreleaser and re-arm the commit-lint hook ([dc8ebf3](https://github.com/sergiught/auth0-mock/commit/dc8ebf356a263a2df769daca3cca79019d930463))
* **release+install:** friendlier latest-release probe, revert section in changelog, full .env.example ([2e07840](https://github.com/sergiught/auth0-mock/commit/2e07840780204720ddff5dd3bf827de080892957))
* **release:** gate docker.io push on DOCKERHUB_USERNAME so GHCR-only release works ([c2f5d8b](https://github.com/sergiught/auth0-mock/commit/c2f5d8bd700cfad69931c02fed2ec978489406ed))
* **renovate:** correct timezone from Europe/Bucharest to Europe/Madrid ([e0e2352](https://github.com/sergiught/auth0-mock/commit/e0e2352e4d4d445fec8d24fbd97381025ced77ed))
* **spec:** wrap matcher schema error, fix godot lint, add tests ([82bce25](https://github.com/sergiught/auth0-mock/commit/82bce25d65b69568968aa2907bbdb763ac3b6c60))
* third-pass audit cleanup — TLS 1.3, password-realm MFA test, docs parity ([85c3d55](https://github.com/sergiught/auth0-mock/commit/85c3d5563e0979c2ea04c7b395b18bdd94d8d2c9))
* **watch+jwks:** source .env in make watch; auto-persist SIGNING_KEY_FILE on first run ([1e91279](https://github.com/sergiught/auth0-mock/commit/1e91279ac3de705babb70c94d17416e8bf43fca8))


### Refactors

* address golangci-lint baseline findings ([7d906bd](https://github.com/sergiught/auth0-mock/commit/7d906bdd7256da0eece0b9eef82887f98f0931dc))
* **admin0:** hoist KindOf, verify stored query matcher in test ([3c87253](https://github.com/sergiught/auth0-mock/commit/3c87253e793f9f198d7b2746e345be45b4e9bb2b))
* **config:** nest tlscert.Config and rename to Specification/Load ([c6b5b92](https://github.com/sergiught/auth0-mock/commit/c6b5b92914e7061b59af7a8e9940dfdd93b3d523))
* **expectations:** drop per-operation match/reset siblings ([284c26c](https://github.com/sergiught/auth0-mock/commit/284c26ccaaa5050341687d0be62086ebf973b3fe))
* **expectations:** drop sibling synthesis from the bundler ([7ed00ba](https://github.com/sergiught/auth0-mock/commit/7ed00ba841d54a1dfc4a6a16877853aaa5bd8eb8))
* log w.Write failures, fix pkce.Entry receiver, t.Parallel sweep ([ff60065](https://github.com/sergiught/auth0-mock/commit/ff60065bf2321a2d89f69fdf5211f164f6b76170))
* **matches:** add request-conditional Expectation store ([791ce5b](https://github.com/sergiught/auth0-mock/commit/791ce5b9aa434222a04bee3b04c9ebe8e93fe152))
* **matches:** harden Store — normalize empty matcher, stable sort ([1702bf2](https://github.com/sergiught/auth0-mock/commit/1702bf2e2b455508b2ca73b9dd54fc0f2bb82d4f))
* **openapi:** decompose mergeFragment and resolve lint findings ([5c30062](https://github.com/sergiught/auth0-mock/commit/5c30062c28b201531ab99e76cd89a2af92c3d525))
* **openapi:** describe match entry shape in admin0 fragment ([9463a69](https://github.com/sergiught/auth0-mock/commit/9463a69c57a6d923fde03be0b92207b123a4e338))
* **openapi:** gofmt service fragment test and validate doc ([39502d5](https://github.com/sergiught/auth0-mock/commit/39502d545cd2aa56ae807a9ad820bccb7e475e2e))
* **openapi:** improve bundler error context and scheme equality ([25ec0a8](https://github.com/sergiught/auth0-mock/commit/25ec0a83556f828c650d557c87ec5cba77174f27))
* **openapi:** thread mgmt prefix into siblings, drop dead params ([1af190e](https://github.com/sergiught/auth0-mock/commit/1af190e7fcf1a44e1c4f7d2433fb110c416b5386))
* **openapi:** tighten mock-control test and embed package doc ([abae123](https://github.com/sergiught/auth0-mock/commit/abae12311a60c856cad653719f5006815b325237))
* **openapi:** use errors.New for static stub error ([acb6ac0](https://github.com/sergiught/auth0-mock/commit/acb6ac04d5fb952db3297faf01dd656f8e6ae3fa))
* **router:** extract /docs page into an embedded HTML file ([dfd932f](https://github.com/sergiught/auth0-mock/commit/dfd932fbc3e45ce1ad868ee4c9875251f09718e3))
* switch to go-chi/chi v5 + render; handlers as ServeHTTP structs ([36d9b18](https://github.com/sergiught/auth0-mock/commit/36d9b18950712cfdd430d83b4c95e0e566170596))


### Tests

* collapse near-identical Verify/ValidateRequestMatcher tests into tables ([049b3ba](https://github.com/sergiught/auth0-mock/commit/049b3ba941e6f7f3c5997de28e385c6f19fe820a))
* **expectations:** cover DELETE error paths; document no-op semantics ([462102a](https://github.com/sergiught/auth0-mock/commit/462102af0cc0c1bf1bda541b83a78448b1951a03))
* **expectations:** migrate godog suite to /admin0/expectations ([c200401](https://github.com/sergiught/auth0-mock/commit/c200401dc04c38f0c1d01cfb730326306096fe30))
* **features:** collapse multi-line JSON path checks into one pattern step ([4e19a1c](https://github.com/sergiught/auth0-mock/commit/4e19a1ceadfd1e444b12deec8c682b123cec9e51))
* **features:** cover Mgmt API CRUD across six core resources ([035a7fa](https://github.com/sergiught/auth0-mock/commit/035a7faef40d0854ed2b56231a4359bfb3dfb493))
* **features:** cover request-conditional expectations ([8012010](https://github.com/sergiught/auth0-mock/commit/80120100aa621c4d27f2a5da3024994420a62588))
* **features:** expand godog coverage from 5 to 33 scenarios ([7a022eb](https://github.com/sergiught/auth0-mock/commit/7a022ebf443d17749c74a33ed79051d109c2f50f))
* **features:** godog runner + scenario context skeleton ([6de0f5c](https://github.com/sergiught/auth0-mock/commit/6de0f5c0f6d776473143e25b7fde198baf4693f1))
* **features:** godog scenarios for match, fallback, bearer, OIDC ([0756b33](https://github.com/sergiught/auth0-mock/commit/0756b332a46be1d5bdd18a2321a7c51ec8fee368))
* **features:** guard empty docstring, clarify helper doc comment ([6f518e3](https://github.com/sergiught/auth0-mock/commit/6f518e3ffeb094046aed5e11dbd9de796833009b))
* **features:** tighten JSON-pattern assertions, drop most PRESENCE markers ([6409f44](https://github.com/sergiught/auth0-mock/commit/6409f44ce3e0c2b05bfb302cc1320b739f135a40))
* JWT alg-confusion + none-alg + malformed, custom-claim override ([540a605](https://github.com/sergiught/auth0-mock/commit/540a605ac099c2b9f5f0220f4496a8cd5e349c55))
* lift authapi + mgmtapi unit coverage to 77.8% ([4d2533d](https://github.com/sergiught/auth0-mock/commit/4d2533d0b4f821e5e0602539bc5923ee2aa7f3df))
* **openapi:** assert runtime spec endpoints stay green ([2a34a74](https://github.com/sergiught/auth0-mock/commit/2a34a74e1081c28d62d0748b33491317662cdfab))
* testify-ify the four files still using raw t.Error/Fatal ([b61173a](https://github.com/sergiught/auth0-mock/commit/b61173a2f4fa64929c4be5177d4e58a7be51ca53))


### Documentation

* add non-affiliation disclaimer and surface /docs in README ([20a1478](https://github.com/sergiught/auth0-mock/commit/20a14783f4a21bd51fbb7f0c738e17c9e0a91fc4))
* call out macOS Go ignoring SSL_CERT_FILE in TLS sections ([dde43a0](https://github.com/sergiught/auth0-mock/commit/dde43a0c6981ed7d2a7ae534a733ce9a0a0283e9))
* **changelog:** drop Keep a Changelog claim — release-please uses its own section taxonomy ([fbd713f](https://github.com/sergiught/auth0-mock/commit/fbd713fde3a3b5bdcf6ca426cecefb1379505033))
* **changelog:** reduce to a stub; release-please takes over from v0.1.0 ([f7a41a3](https://github.com/sergiught/auth0-mock/commit/f7a41a3918aa279d180d5200959ee4b8e9de6e8f))
* **contributing:** switch test snippets to make targets + document coverage + drop CHANGELOG hand-edits ([b4da465](https://github.com/sergiught/auth0-mock/commit/b4da46585c644bafd74fe09e943d5675e6aed9af))
* convert blockquote callouts to GitHub alert syntax ([a606fa2](https://github.com/sergiught/auth0-mock/commit/a606fa2dac94ae877512b0efe6c59b18c96b2db1))
* correct strip-set rationale, lock in encoded/Unicode allowed cases, flag install.sh pre-v0.1.0 ([feb20b8](https://github.com/sergiught/auth0-mock/commit/feb20b82bea1a257fef4eda5cd32b3c697e45503))
* document new lint, security, and commit-shape tooling ([2be72a4](https://github.com/sergiught/auth0-mock/commit/2be72a41b9f012a79008de78e11039f69e59a00e))
* drop COMPARISON.md and its references ([5676351](https://github.com/sergiught/auth0-mock/commit/567635117c8fcec9c1a5abada91736a53689548f))
* drop competitor references ([07e7e53](https://github.com/sergiught/auth0-mock/commit/07e7e53dac4c94d5b0fec6cc2c1b2dd426c4be9f))
* drop em-dashes in favour of colons, commas, and periods ([66bfb06](https://github.com/sergiught/auth0-mock/commit/66bfb0672bf7bf2c0e5e3b76f77bb1db52e2d6a1))
* examples/consumer demonstrates the OIDC + mgmt loop ([0796754](https://github.com/sergiught/auth0-mock/commit/0796754203452ba66df44327bf484ca30bfe59b5))
* **expectations:** clarify precedence — path concreteness is primary ([6db98ab](https://github.com/sergiught/auth0-mock/commit/6db98abc94c9156556ee8728151a9fb2c1c01a05))
* **expectations:** correct precedence prose and error-code coverage ([4efe817](https://github.com/sergiught/auth0-mock/commit/4efe81743ca5a91034e5e0eabe4c9458e18e974d))
* **expectations:** document nested request+response payload ([5f1ba13](https://github.com/sergiught/auth0-mock/commit/5f1ba13c28bb45bf35b6dacd183606d03a5d6f31))
* **expectations:** document the centralized /admin0/expectations API ([1084405](https://github.com/sergiught/auth0-mock/commit/10844058dba0f13709736cd658b24ce65cc48e68))
* **expectations:** fix stale MatchRegistration.feature reference ([2c41a93](https://github.com/sergiught/auth0-mock/commit/2c41a93224b81bb6df1bba957beb9684eea107e6))
* **expectations:** fix stale sibling refs in stripUpstreamProse comment ([5717277](https://github.com/sergiught/auth0-mock/commit/571727728fd33ebd79882883fb121d07dc833e26))
* **expectations:** reconcile CHANGELOG and tidy expectations vocabulary ([49c5826](https://github.com/sergiught/auth0-mock/commit/49c5826dddb64c47b939fcdc2ebd9d337086ef9b))
* humanize prose across README and docs/ ([30ce5e9](https://github.com/sergiught/auth0-mock/commit/30ce5e9663fe83f3f5bf060de6a69678d32f7e87))
* **notice:** tighten Auth0 spec attribution and refresh copyright year ([e6de6d9](https://github.com/sergiught/auth0-mock/commit/e6de6d98d8ad570bbe60138abfdd415e790332d2))
* **openapi:** add OpenAPI export section to ARCHITECTURE.md ([69f7684](https://github.com/sergiught/auth0-mock/commit/69f768435b6d5c5a0604f5f576c3e24e91156c1e))
* **openapi:** correct docs to reflect the skeleton, not a verbatim spec ([a952f33](https://github.com/sergiught/auth0-mock/commit/a952f33dc7d2f1ceceeada8abcc19545587403d9))
* **openapi:** document Postman and Insomnia import flow ([b22319f](https://github.com/sergiught/auth0-mock/commit/b22319f8f21e4649e585e9c44399a875918b2d2e))
* production Dockerfile and full README ([3ad46d2](https://github.com/sergiught/auth0-mock/commit/3ad46d2466b12a5aff1c6a1323488d5c4754b8ae))
* **readme:** add a table of contents with back-links per section ([80aef3f](https://github.com/sergiught/auth0-mock/commit/80aef3fce6089ef3cdf2b5052e264d5b2276dfd9))
* top-class README + ARCHITECTURE + COOKBOOK + COMPARISON + CONTRIBUTING + CHANGELOG ([2ac7db4](https://github.com/sergiught/auth0-mock/commit/2ac7db4727cca47ea83e9377e7f13d9bd0786aef))
* truthing pass — readyz, NOTICE, compose claim, env example, jwks iat doc ([e1ed7f3](https://github.com/sergiught/auth0-mock/commit/e1ed7f39aae6f175506fb8a98d50140992dc200f))
* verifying-releases section + audience-not-enforced note ([5d1899b](https://github.com/sergiught/auth0-mock/commit/5d1899b22579bb5357e55da4dcb79ef7f7e50c3a))


### Build

* add Makefile, docker-compose, dev Dockerfile ([9b667b1](https://github.com/sergiught/auth0-mock/commit/9b667b12257ba6d5cd44f9e3228f670ee57d5602))
* collapse to a single production Dockerfile at repo root ([3508b70](https://github.com/sergiught/auth0-mock/commit/3508b70cabe0582dd464d4d94dbd7e46b4b50925))
* **commitlint:** add commitlint v0.10.1 (config + Makefile target) ([ea76323](https://github.com/sergiught/auth0-mock/commit/ea7632348ecf762d3ece8b55363bdc3e851128c0))
* **dev:** native hot-reload via 'make watch' + air ([dfcd670](https://github.com/sergiught/auth0-mock/commit/dfcd67060a20e9237fdaf00e126d500a8c1ffccf))
* **lint:** add golangci-lint v2.5.0 (config + Makefile target) ([848ce53](https://github.com/sergiught/auth0-mock/commit/848ce5337eb901e9685b6c072be3c34ebf429856))
* **make:** default to a help target listing every command ([0cc19dd](https://github.com/sergiught/auth0-mock/commit/0cc19dd38aaf7dffa6e4b4581ae10a5aeeb086e4))
* **openapi:** add make target and bundler skeleton ([c0f9b12](https://github.com/sergiught/auth0-mock/commit/c0f9b12dd9026e50d504bec0e2e24d916465756b))
* **precommit:** add pre-commit framework hooks ([86de1ea](https://github.com/sergiught/auth0-mock/commit/86de1ea83f61f7ba5e1c8e8d887e61e5c72c7099))
* **vuln:** add govulncheck Makefile target ([82e12b2](https://github.com/sergiught/auth0-mock/commit/82e12b2b297c551bbdb43a6c8f2664ca10cd23b8))


### Chores

* pin first release at 0.225.0 (one per commit so far) ([48d5490](https://github.com/sergiught/auth0-mock/commit/48d5490e256378b4ff91bc95345634112a875a8e))

## Changelog

All notable changes are tracked in [GitHub Releases](https://github.com/sergiught/auth0-mock/releases) and generated into this file by [release-please](https://github.com/googleapis/release-please) from conventional-commit messages. Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
