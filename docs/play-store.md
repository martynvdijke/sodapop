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
