# Google Play submission

What the Play Console asks for, and the answers that match what Sodapop actually
does. Update this file whenever a permission, SDK or server feature changes —
the Data safety form is a legal declaration, not a formality.

## Before the first upload

| Requirement | Where | Sodapop answer |
| --- | --- | --- |
| App must exist | Play Console → Create app | Name, default language, "App" + "Free" |
| Privacy policy URL | Store listing → Privacy policy | `https://<your-host>/privacy` — must load without signing in |
| Account deletion URL | App content → Data deletion | `https://<your-host>/account/delete` |
| Data deletion in app | App content → Data deletion | Yes: Settings → Account → Delete account |
| Sign-in details | App content → App access | Test account credentials, or "all functionality is restricted" if the instance is invite-only |

Play requires the deletion URL to be reachable *and* to explain what happens, not
just to exist. `/account/delete` does both.

## Data safety answers

The Android app talks to `/api/v1` only. These answers assume the default build:
no crash reporting, no advertising SDK, no third-party analytics.

**Does your app collect or share any of the required user data types?** Yes.

| Data type | Collected | Shared | Purpose | Optional? | Ephemeral? |
| --- | --- | --- | --- | --- | --- |
| Personal info → Email address | Yes | No | Account management | No (required to register) | No |
| Personal info → Name | Yes (handle, display name) | No | Account management, app functionality | No | No |
| Personal info → User IDs | Yes (account id) | No | Account management | No | No |
| Location → Approximate location | Yes | No | App functionality (find nearby venues) | Yes (only when you tap "find nearby") | **Yes** |
| Location → Precise location | Yes | No | App functionality (find nearby venues) | Yes (same gesture) | **Yes** |
| Photos and videos → Photos | Yes | No | App functionality (check-in, avatar) | Yes | No |
| App activity → Other user-generated content | Yes (check-ins, notes, comments) | No | App functionality | Yes | No |
| App activity → Other actions | Yes (ratings, saves, lists, badges) | No | App functionality | No | No |
| Device or other IDs | Yes (FCM push token), only if push is enabled | No | App functionality (notifications) | Yes | No |

Notes that matter during review:

- **Location is ephemeral.** Coordinates are posted to `/nearby` for a single
  query and never written to the database. Check that stays true before
  ticking the ephemeral box.
- **"Shared" means transferred to a third party.** The FCM push service delivers
  notifications but is the platform channel for the app's own notifications, so
  it counts as collection, not sharing.
- **Nothing is sold**, so "sharing for advertising" is No everywhere.
- Photos and notes are visible to other users according to the visibility the
  author picks, which is why the app-activity row is "collected", not "shared
  with third parties".

## Permissions and in-app safety

Play's User Data and UGC policies are checked against behaviour, not against the
policy text, so each permission has to match a gesture:

| Permission | Asked when | Rationale the reviewer can see |
| --- | --- | --- |
| Camera | Opening the Scan tab, which builds the scanner on demand | The tab exists to scan barcodes; the manual GTIN field works if permission is refused |
| Location (approximate + precise) | Tapping "Use my location" on the Nearby screen | A card above the button says coordinates are sent once and never stored |
| Notifications | Turning on the push switch in Settings | Nothing is registered unless the user asks; a build without FCM config labels the switch as unconfigured |

The UGC policy expects both reporting and blocking to exist inside the app:

- **Report** appears in the overflow menu of every check-in, every comment the
  viewer did not write, and every profile. Reports land in the web moderation
  queue at `/admin`.
- **Block** is on the profile and in Settings → Blocked accounts, and removes
  friendships and follows in both directions.
- Account deletion is reachable from Settings → Account, and the same URL is
  submitted as the Data deletion link.

A manual `flutter build appbundle --release` is signed only if
`android/key.properties` exists, and it inherits the version code from
`pubspec.yaml`, which semantic-release pins at `+1`. Pass
`--build-number=$(git rev-list --count HEAD)` when building by hand, or Play will
reject the upload as a downgrade.

`android:allowBackup="false"` plus `res/xml/data_extraction_rules.xml` keep the
bearer token and the offline check-in queue out of cloud backups and
device-to-device transfers. Play does not require this, but a backed-up session
token is a data-protection problem the Data safety form would have to disclose.

## Content rating

Questionnaire answers: no violence, no sexual content, no profanity, no
gambling, no controlled substances. **Alcohol and tobacco references: yes** —
this is a drinks app. Age rating lands at 12+/Teen. Do not answer "no" to the
substance question to get a lower rating; that is a misdeclaration.

## Ads and target audience

- Contains ads: No.
- Target age group: 16+ (the privacy policy commits to 16, matching the GDPR
  default for consent in most member states).
- Not designed for children; do not opt into the Families programme.

## Testing requirements

Personal developer accounts created after November 2023 must run a closed test
with at least 12 testers for 14 continuous days before production access is
granted. Internal testing does not count towards it.

- Internal testing: up to 100 testers, no review, link is
  `https://play.google.com/apps/internaltest/<id>` from the Testers tab.
- Closed testing: `https://play.google.com/apps/testing/<package>`, up to 1,000
  testers, needs a review pass.

## Uploading

The release workflow builds `app-release.aab` and publishes it to the track in
`vars.GOOGLE_PLAY_TRACK` (default `internal`) with `vars.GOOGLE_PLAY_RELEASE_STATUS`
(default `completed`), so a normal release needs no manual step. A manual upload
is only needed for the very first bundle of a brand-new app, which Play requires
through the Console.

## Service account

Play Console → Users and permissions must include the service account from
`GOOGLE_PLAY_SERVICE_ACCOUNT_JSON`, with *Release to testing tracks* at minimum.
Without that grant the Publisher API answers 404 for the package even when the
credentials are valid — a missing grant looks exactly like a missing app.
