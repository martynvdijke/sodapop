# [1.28.0](https://github.com/martynvdijke/sodapop/compare/v1.27.0...v1.28.0) (2026-11-10)


### Bug Fixes

* **db:** keep SQLite views intact across Ent schema rebuilds ([9c9dd35](https://github.com/martynvdijke/sodapop/commit/9c9dd358b533bcaa5632fb97941b21ce4908fd78))


### Features

* **web:** restyle the web app to match the Android app ([261cf89](https://github.com/martynvdijke/sodapop/commit/261cf890d81def284a4ed924ac07a6746a0af8b2))

# [1.27.0](https://github.com/martynvdijke/sodapop/compare/v1.26.2...v1.27.0) (2026-11-09)


### Features

* check in at home and see nearby places ([0263db8](https://github.com/martynvdijke/sodapop/commit/0263db859541770fce46bd512d5d492a38ef0f05))
* suggest drink pairings on the drink page ([f63054b](https://github.com/martynvdijke/sodapop/commit/f63054b7f3ce36d394d48172d8d88fabd5cd5e8b))

## [1.26.2](https://github.com/martynvdijke/sodapop/compare/v1.26.1...v1.26.2) (2026-11-09)


### Bug Fixes

* **trmnl:** pin minted plugin ids ([b59a726](https://github.com/martynvdijke/sodapop/commit/b59a7260acb834c3727da8b3d0dbe1e8b10dcce0))

## [1.26.1](https://github.com/martynvdijke/sodapop/compare/v1.26.0...v1.26.1) (2026-11-09)


### Bug Fixes

* **trmnl:** shorten My Stats description for trmnlp lint ([cc458c7](https://github.com/martynvdijke/sodapop/commit/cc458c7c6e72950f0992a3c87cb15e5b009791af))

# [1.26.0](https://github.com/martynvdijke/sodapop/compare/v1.25.0...v1.26.0) (2026-11-09)


### Features

* support tea, coffee and 0.0% drinks with prep details ([db3ed1f](https://github.com/martynvdijke/sodapop/commit/db3ed1f1613c1afd54e3f4c70ab76d438a512291))
* **trmnl:** add e-ink plugins backed by a dedicated TRMNL API ([759ed21](https://github.com/martynvdijke/sodapop/commit/759ed21347272a5e00e31dc306aebab554868e8d))

# [1.25.0](https://github.com/martynvdijke/sodapop/compare/v1.24.0...v1.25.0) (2026-11-08)


### Bug Fixes

* let accepted friends read private check-ins ([2acfcf7](https://github.com/martynvdijke/sodapop/commit/2acfcf73f5172433a1a02f75d6f642ba7417c66f))


### Features

* add an admin activity overview on web and Android ([33da8a1](https://github.com/martynvdijke/sodapop/commit/33da8a13ad9214e4ce56ab96a26e8525771fae2a))

# [1.24.0](https://github.com/martynvdijke/sodapop/compare/v1.23.0...v1.24.0) (2026-11-08)


### Features

* **telemetry:** export logs, trace DB queries, and add Prometheus metrics ([f539400](https://github.com/martynvdijke/sodapop/commit/f539400a8c8ac4d29410d9a5fb19a2bf27b3af8d))
* **web:** show avatars in nav and profile, allow removing a photo ([3df718b](https://github.com/martynvdijke/sodapop/commit/3df718b01002c2091ac3b3963ebe4ebf5ca87b58))

# [1.23.0](https://github.com/martynvdijke/sodapop/compare/v1.22.1...v1.23.0) (2026-11-08)


### Features

* **mobile:** add OpenTelemetry tracing with runtime endpoint from API ([047491c](https://github.com/martynvdijke/sodapop/commit/047491c8dda4ee9b3a89462d4e925076ac5e332e))

## [1.22.1](https://github.com/martynvdijke/sodapop/compare/v1.22.0...v1.22.1) (2026-11-07)


### Bug Fixes

* **deps:** update module modernc.org/sqlite to v1.62.0 ([#16](https://github.com/martynvdijke/sodapop/issues/16)) ([f7585a3](https://github.com/martynvdijke/sodapop/commit/f7585a31e0a18fe6f5d427871271e2a4974c5c1f))

# [1.22.0](https://github.com/martynvdijke/sodapop/compare/v1.21.1...v1.22.0) (2026-11-07)


### Bug Fixes

* keep Android scanner preview alive after a scan ([98a9051](https://github.com/martynvdijke/sodapop/commit/98a90510c00d96d25671f15574e9a87b54c6d7bf))


### Features

* add profile photos, pinned drinks, and check-in filters ([0db5a7e](https://github.com/martynvdijke/sodapop/commit/0db5a7efb420725d381dc6998921ea59070cc8f6))

## [1.21.1](https://github.com/martynvdijke/sodapop/compare/v1.21.0...v1.21.1) (2026-11-06)

# [1.21.0](https://github.com/martynvdijke/sodapop/compare/v1.20.1...v1.21.0) (2026-11-06)


### Bug Fixes

* keep barcode lookup working when OFF changes its response shape ([c67c897](https://github.com/martynvdijke/sodapop/commit/c67c897ddb3696433d57abe50d0744c922ae6245))
* **mobile:** start the scanner reliably and explain camera failures ([af1df06](https://github.com/martynvdijke/sodapop/commit/af1df06b4fe2af0bfbbb7f4c902939ad51b70627))


### Features

* **mobile:** show own and friends' posts in a Feed tab ([2bbfce2](https://github.com/martynvdijke/sodapop/commit/2bbfce2309fe7e8e38267cf9385df9216b85ab42))

## [1.20.1](https://github.com/martynvdijke/sodapop/compare/v1.20.0...v1.20.1) (2026-11-05)


### Bug Fixes

* normalize container before provisional drink dedupe ([85b9ee3](https://github.com/martynvdijke/sodapop/commit/85b9ee38c98d9f780de9511c49290ef6dcc72d53))

# [1.20.0](https://github.com/martynvdijke/sodapop/compare/v1.19.0...v1.20.0) (2026-11-05)


### Features

* expand scan-to-check-in flow and public preview ([1123a33](https://github.com/martynvdijke/sodapop/commit/1123a3315d6b11e2703a29c7897c15dd97e018d0))

# [1.19.0](https://github.com/martynvdijke/sodapop/compare/v1.18.0...v1.19.0) (2026-11-05)


### Features

* **mobile:** add friend request action to profiles ([9117175](https://github.com/martynvdijke/sodapop/commit/91171752929ac24d47d7f94067a85541aa7f5925))

# [1.18.0](https://github.com/martynvdijke/sodapop/compare/v1.17.3...v1.18.0) (2026-11-05)


### Features

* **mobile:** add friend action to friends feed ([d71d41b](https://github.com/martynvdijke/sodapop/commit/d71d41b4311ea5ee25e6849364848cec5d774a5a))

## [1.17.3](https://github.com/martynvdijke/sodapop/compare/v1.17.2...v1.17.3) (2026-11-03)

## [1.17.2](https://github.com/martynvdijke/sodapop/compare/v1.17.1...v1.17.2) (2026-11-03)


### Bug Fixes

* use a bare envelope sender for outgoing mail ([70216c6](https://github.com/martynvdijke/sodapop/commit/70216c67722f4f2b29758ec510b84570107f31a8))

## [1.17.1](https://github.com/martynvdijke/sodapop/compare/v1.17.0...v1.17.1) (2026-11-02)

# [1.17.0](https://github.com/martynvdijke/sodapop/compare/v1.16.0...v1.17.0) (2026-11-02)


### Features

* **moderation:** let an admin undo a suspension ([8d0a81a](https://github.com/martynvdijke/sodapop/commit/8d0a81a1c858f70a585fe2c812b490b3cd5207da))

# [1.16.0](https://github.com/martynvdijke/sodapop/compare/v1.15.0...v1.16.0) (2026-11-01)


### Features

* **moderation:** let moderators act on what gets reported ([36de520](https://github.com/martynvdijke/sodapop/commit/36de52016875215b10ddcdf87db923eb66ba427a))

# [1.15.0](https://github.com/martynvdijke/sodapop/compare/v1.14.0...v1.15.0) (2026-11-01)


### Features

* close the Google Play policy gaps ([70f52df](https://github.com/martynvdijke/sodapop/commit/70f52dff803edede416dc86d9dfb4d444f5b26c3))

# [1.14.0](https://github.com/martynvdijke/sodapop/compare/v1.13.4...v1.14.0) (2026-11-01)


### Features

* **legal:** publish privacy, terms and deletion pages ([e6fd1c3](https://github.com/martynvdijke/sodapop/commit/e6fd1c3c865ebde98d87fc6667cf92d9e8296dd9))

## [1.13.4](https://github.com/martynvdijke/sodapop/compare/v1.13.3...v1.13.4) (2026-11-01)


### Bug Fixes

* **ci:** keep the Android version code above what Play already has ([be79880](https://github.com/martynvdijke/sodapop/commit/be79880943d1f4cafeabd6a9e5194ff0944238c0))

## [1.13.3](https://github.com/martynvdijke/sodapop/compare/v1.13.2...v1.13.3) (2026-11-01)


### Bug Fixes

* **web:** make the admin badge, digest and system pages self-explanatory ([90411f9](https://github.com/martynvdijke/sodapop/commit/90411f932320f3b7cb80b8dc26cd0e78fabb9dd0))

## [1.13.2](https://github.com/martynvdijke/sodapop/compare/v1.13.1...v1.13.2) (2026-11-01)


### Bug Fixes

* **ci:** skip Play Store publish when credentials are missing ([ccb10c6](https://github.com/martynvdijke/sodapop/commit/ccb10c68b0892670ad7e1a45c2aeae314031d5f9))

## [1.13.1](https://github.com/martynvdijke/sodapop/compare/v1.13.0...v1.13.1) (2026-10-31)

# [1.13.0](https://github.com/martynvdijke/sodapop/compare/v1.12.2...v1.13.0) (2026-10-31)


### Bug Fixes

* **deps:** update module modernc.org/sqlite to v1.61.1 ([#12](https://github.com/martynvdijke/sodapop/issues/12)) ([f4d3504](https://github.com/martynvdijke/sodapop/commit/f4d3504a387c316e3fb3e5ec2b194eaad44f4991))


### Features

* **mobile:** default to sodapop.vandijke.xyz and offer self-hosted option ([e5e8bc3](https://github.com/martynvdijke/sodapop/commit/e5e8bc3086cd7e594d400552c0cc5061ba8f463b))

## [1.12.2](https://github.com/martynvdijke/sodapop/compare/v1.12.1...v1.12.2) (2026-10-30)

## [1.12.1](https://github.com/martynvdijke/sodapop/compare/v1.12.0...v1.12.1) (2026-10-29)

# [1.12.0](https://github.com/martynvdijke/sodapop/compare/v1.11.0...v1.12.0) (2026-10-29)


### Features

* honor LOG_LEVEL and surface push credentials in admin settings ([2d8936d](https://github.com/martynvdijke/sodapop/commit/2d8936dec4ff2c5e2ce762243720bfc218275924))

# [1.11.0](https://github.com/martynvdijke/sodapop/compare/v1.10.0...v1.11.0) (2026-10-29)


### Features

* add share cards, taste queue, live notifications, delivery history and FCM push ([3ba3f09](https://github.com/martynvdijke/sodapop/commit/3ba3f09afe8d739b1595ef7466cd57fc567a4cec))

# [1.10.0](https://github.com/martynvdijke/sodapop/compare/v1.9.2...v1.10.0) (2026-10-28)


### Features

* add badge levels, streaks, activity timeline and venue offers ([3705250](https://github.com/martynvdijke/sodapop/commit/3705250e79dfd5eeec8f9f48f8153d5460b40df5))

## [1.9.2](https://github.com/martynvdijke/sodapop/compare/v1.9.1...v1.9.2) (2026-10-28)

## [1.9.1](https://github.com/martynvdijke/sodapop/compare/v1.9.0...v1.9.1) (2026-10-28)


### Bug Fixes

* **ci:** skip Play Store publishing without credentials ([3d31dcb](https://github.com/martynvdijke/sodapop/commit/3d31dcbdc37cfdb960853724a037b58bbe0ea931))

# [1.9.0](https://github.com/martynvdijke/sodapop/compare/v1.8.0...v1.9.0) (2026-10-28)


### Bug Fixes

* **ci:** use the repository Flutter version in Play Store builds ([0abbb7f](https://github.com/martynvdijke/sodapop/commit/0abbb7f24842f6b3361f16468718a08df1302532))


### Features

* add account lifecycle, nearby feed and public drink API ([3f679dc](https://github.com/martynvdijke/sodapop/commit/3f679dcc9282c939f5332e47755d19acf14bb233))

# [1.8.0](https://github.com/martynvdijke/sodapop/compare/v1.7.0...v1.8.0) (2026-10-28)


### Features

* complete mobile parity and post interactions ([3f485a2](https://github.com/martynvdijke/sodapop/commit/3f485a2b3833a88de594e3925bb0623aa85e131e))

# [1.7.0](https://github.com/martynvdijke/sodapop/compare/v1.6.0...v1.7.0) (2026-10-28)


### Features

* add backups, admin ops and starter catalog ([820c802](https://github.com/martynvdijke/sodapop/commit/820c802b6ad5198684152bcd0f22770a9b741804))

# [1.6.0](https://github.com/martynvdijke/sodapop/compare/v1.5.2...v1.6.0) (2026-10-27)


### Features

* add post bookmarks, mentions, pagination and reporting ([e7818f2](https://github.com/martynvdijke/sodapop/commit/e7818f2d700d339804d5fcac3f5782230b72ca14))

## [1.5.2](https://github.com/martynvdijke/sodapop/compare/v1.5.1...v1.5.2) (2026-10-27)

## [1.5.1](https://github.com/martynvdijke/sodapop/compare/v1.5.0...v1.5.1) (2026-10-27)


### Bug Fixes

* **deps:** update dependency cupertino_icons to v2 ([#3](https://github.com/martynvdijke/sodapop/issues/3)) ([bcaebfc](https://github.com/martynvdijke/sodapop/commit/bcaebfcd37e0072f9a62e671a28b081943055ebd))

# [1.5.0](https://github.com/martynvdijke/sodapop/compare/v1.4.3...v1.5.0) (2026-10-27)


### Features

* **release:** publish android bundles to google play ([fbf5856](https://github.com/martynvdijke/sodapop/commit/fbf5856dd7517454179f959ea6c8ace1f9646f59))

## [1.4.3](https://github.com/martynvdijke/sodapop/compare/v1.4.2...v1.4.3) (2026-10-27)


### Bug Fixes

* add Android barcode scanner emulator coverage ([ce0a03d](https://github.com/martynvdijke/sodapop/commit/ce0a03d8e1f09ef665b331d076946decc626c2d1))

## [1.4.2](https://github.com/martynvdijke/sodapop/compare/v1.4.1...v1.4.2) (2026-10-26)


### Bug Fixes

* derive the footer version from the semantic-release version ([c6062e0](https://github.com/martynvdijke/sodapop/commit/c6062e0a1519b62358c5428dbc4ae3948b9fe09b))

## [1.4.1](https://github.com/martynvdijke/sodapop/compare/v1.4.0...v1.4.1) (2026-10-26)


### Bug Fixes

* load the progressive enhancement bundle as a module ([0b95130](https://github.com/martynvdijke/sodapop/commit/0b95130f0d0f64517c4396e44dcc4b48d1f2fe0e))

# [1.4.0](https://github.com/martynvdijke/sodapop/compare/v1.3.0...v1.4.0) (2026-10-26)


### Features

* add durable delivery, scheduler leases, metrics, unified search, and admin completeness ([975c444](https://github.com/martynvdijke/sodapop/commit/975c4445c1343d93af3e2fcae322285f49ce9524))

# [1.3.0](https://github.com/martynvdijke/sodapop/compare/v1.2.0...v1.3.0) (2026-10-26)


### Features

* add comments, venue media, comparison, and richer profiles (phase B) ([b2c7bae](https://github.com/martynvdijke/sodapop/commit/b2c7bae4c4504fbea188a40450da00db78d193ae))
* add first-run admin setup and runtime settings ([e73490a](https://github.com/martynvdijke/sodapop/commit/e73490a829a713342b87fe39e15f73ae852fd87a))
* add safety controls, galleries, AI drafts, audit, and admin setup foundation (phase C) ([53e0c87](https://github.com/martynvdijke/sodapop/commit/53e0c87f8d6cde9ea863d20755836bcc90c9bc5b))
* complete v1.2 API and Android parity (phase D) ([16751b0](https://github.com/martynvdijke/sodapop/commit/16751b05c0f2a6b4f199080eaa7ee8983c5565e9))
* complete v1.2 surface wiring (phase A) ([811a6bf](https://github.com/martynvdijke/sodapop/commit/811a6bf29ae3463dabf4a4b0496b979547b7f8fe))

# [1.2.0](https://github.com/martynvdijke/sodapop/compare/v1.1.0...v1.2.0) (2026-10-25)


### Features

* ship v1.1 member surfaces ([cdbf748](https://github.com/martynvdijke/sodapop/commit/cdbf748ae47041f229a0fc59702e3e52db7c2c95))

# [1.1.0](https://github.com/martynvdijke/sodapop/compare/v1.0.3...v1.1.0) (2026-10-25)


### Features

* complete v1 with photos, AI, PWA and integrations ([1779a5b](https://github.com/martynvdijke/sodapop/commit/1779a5bb7b50bf76c697f4ddfa92ff73f77910d6))

## [1.0.3](https://github.com/martynvdijke/sodapop/compare/v1.0.2...v1.0.3) (2026-10-25)


### Bug Fixes

* **ci:** allow manual release runs ([043ff68](https://github.com/martynvdijke/sodapop/commit/043ff687ba890531abeb81314ff29b861cc0314b))

## [1.0.2](https://github.com/martynvdijke/sodapop/compare/v1.0.1...v1.0.2) (2026-10-25)


### Bug Fixes

* **ci:** keep OpenSpec notes local and fix Flutter setup ([d199e2e](https://github.com/martynvdijke/sodapop/commit/d199e2e3d4b2332d05a08b57db5817b6a1479b19))

## [1.0.1](https://github.com/martynvdijke/sodapop/compare/v1.0.0...v1.0.1) (2026-10-25)


### Bug Fixes

* **ci:** keep release green without optional integrations ([49b5b84](https://github.com/martynvdijke/sodapop/commit/49b5b84fb5ea10c0a3bfe3ddff8972c041c59e69))

# 1.0.0 (2026-10-25)


### Features

* build barcode-first soda check-in app ([01899dd](https://github.com/martynvdijke/sodapop/commit/01899dd2c28b3788f88430d0714a3e8f16b1089b))
