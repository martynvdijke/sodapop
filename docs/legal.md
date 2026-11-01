# Legal pages

Sodapop ships the three pages a hosted, account-based service needs: a privacy
policy, terms of use, and a deletion page. They are public routes, reachable
without signing in, because a store reviewer and a prospective user both need
to read them before they have an account.

| Route | Template | Purpose |
| --- | --- | --- |
| `GET /privacy` | `pages/privacy.html` | What is stored, why, who processes it, retention, rights |
| `GET /terms` | `pages/terms.html` | Account rules, content licence, availability, liability |
| `GET /account/delete` | `pages/account_delete.html` | How to delete, and what happens to the data |

## Configuration

The pages render sensible text with no configuration at all, but an instance that
collects real data should name its operator. All four values are env-only
(restart to apply), never database-backed:

```sh
LEGAL_CONTROLLER_NAME="Example Soda Cooperative"
LEGAL_CONTACT_EMAIL=privacy@example.com
LEGAL_CONTACT_ADDRESS="1 Fizz Street, 1000 AA Amsterdam"
LEGAL_HOSTING_REGION="the Netherlands"
```

- `LEGAL_CONTROLLER_NAME` is the GDPR "controller". Empty falls back to
  "the operator of this <AppName> instance".
- `LEGAL_CONTACT_EMAIL` adds a `mailto:` contact and enables the email fallback
  on the deletion page. Empty removes both rather than inventing an address.
- `LEGAL_CONTACT_ADDRESS` is optional; a postal address is good practice but not
  mandatory in the text.
- `LEGAL_HOSTING_REGION` describes where the server runs, for the transfer
  paragraph.

The policy also adapts to the instance: the analytics paragraph appears only when
`UMAMI_URL` is set, the tracing paragraph only when `OTEL_ENDPOINT` is set, the
mail processor only when SMTP is configured, push only when VAPID or FCM is
configured, and the login-provider processor only when OIDC is enabled. An
instance cannot accidentally claim a processor it is not running.

## What deletion actually does

`POST /settings/delete` (web) and `DELETE /api/v1/me` (API) both go through
`account.Service.Delete`, and both require the handle as a confirmation token.
The sequence is:

1. Mark the row `status=deleted` with `deleted_at`, so the account can no longer
   sign in and its content is no longer attributed to a live user.
2. Revoke every session, API token, inbound token and share link, and delete the
   push devices.
3. Anonymise the row: email becomes `deleted-<id>@deleted.invalid`, the handle
   becomes `deleted-<id>`, the display name becomes "Deleted user", and the bio,
   country, avatar, password hash and verification timestamp are cleared.
4. Delete identities, follows, friendships, blocks and notifications.

Content is deliberately kept. Check-ins, comments and photos are referenced by
other people's feeds and conversations, so erasing them would corrupt data the
user does not own alone. Detaching them from the identity satisfies erasure
("stop processing this person's data") while leaving the shared record intact —
this is what the privacy policy and the deletion page both describe, so change
the copy if you change the behaviour.

The last live admin cannot delete themselves; promote someone else first.

## Checklist for an EU launch

- [ ] Set all four `LEGAL_*` values, then open `/privacy` and confirm they render.
- [ ] Confirm `BASE_URL` is the public HTTPS origin; the policy links are relative.
- [ ] Sign a data processing agreement with the hosting provider.
- [ ] Check the hosting region matches `LEGAL_HOSTING_REGION` and that transfers
      outside the EEA (if any) rely on SCCs.
- [ ] Decide a retention period for rotated backups and write it down; the policy
      says backups rotate out without promising a number.
- [ ] Keep a record of processing activities — for a single-instance deployment
      this can be a page in your own docs.
- [ ] Publish the deletion URL in the Play listing (see `docs/play-store.md`).
