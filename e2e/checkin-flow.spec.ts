import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { createToken, newUser, registerAndVerify, type TestUser } from './helpers';

/**
 * The composer enhancements (geolocation, venue picker, live photo preview)
 * only run with the built web assets. These tests exercise the real browser
 * flow against the seeded server.
 */
// Keep the fix away from nearby.spec.ts's 51.5,-0.12. Venue creation dedupes
// any two venues within 150 m, so sharing the same point across specs would
// merge their fixtures and make the suites order-dependent.
const LAT = 52.37;
const LNG = 4.895;

test.use({
  geolocation: { latitude: LAT, longitude: LNG },
  permissions: ['geolocation'],
});

const auth = (token: string) => ({ Authorization: `Bearer ${token}` });

const UNKNOWN_GTIN = '5901234123457';

// A 2x2 PNG fixture. The server decodes every upload to make a thumbnail, so
// the bytes must be a real image rather than an extension hint.
const PHOTO_FIXTURE = 'e2e/fixtures/pour.png';

function unique(prefix: string): string {
  const suffix = `${Date.now().toString(36)}${Math.floor(Math.random() * 1e4)}`;
  return `${prefix} ${suffix}`;
}

function listFrom<T>(payload: unknown, upper: string, lower: string): T[] {
  const data = (payload as { data?: Record<string, unknown> } | null)?.data;
  return ((data?.[upper] ?? data?.[lower] ?? []) as T[]) ?? [];
}

/** Sends a request from B and accepts it as A through the personal API. */
async function becomeFriends(
  request: APIRequestContext,
  alicePage: Page,
  bobPage: Page,
  alice: TestUser,
  bob: TestUser,
): Promise<void> {
  const aliceToken = await createToken(alicePage);
  const bobToken = await createToken(bobPage);

  const sent = await request.post('/api/v1/friends/requests', {
    headers: auth(bobToken),
    data: { handle: alice.handle },
  });
  expect([200, 202]).toContain(sent.status());

  const pendingRes = await request.get('/api/v1/friends', { headers: auth(aliceToken) });
  expect(pendingRes.status()).toBe(200);
  const pendingBody = await pendingRes.json();
  const incoming = listFrom<{ id: string; handle: string }>(pendingBody, 'Incoming', 'incoming');
  const pending = incoming.find((row) => row.handle === bob.handle);
  expect(pending, JSON.stringify(pendingBody)).toBeTruthy();

  const accepted = await request.post(`/api/v1/friends/requests/${pending!.id}/accept`, {
    headers: auth(aliceToken),
  });
  expect([200, 204]).toContain(accepted.status());

  const friendsRes = await request.get('/api/v1/friends', { headers: auth(aliceToken) });
  expect(friendsRes.status()).toBe(200);
  const friendsBody = await friendsRes.json();
  const friends = listFrom<{ id: string; handle: string }>(friendsBody, 'Friends', 'friends');
  expect(friends.some((row) => row.handle === bob.handle), JSON.stringify(friendsBody)).toBe(true);
}

test('an unknown manual barcode opens the add-drink form and the soda is checkable', async ({ page }) => {
  await registerAndVerify(page);

  await page.goto('/scan');
  const scannerForm = page.locator('.scanner-manual');
  await scannerForm.getByLabel('Barcode digits').fill(UNKNOWN_GTIN);
  await scannerForm.getByRole('button', { name: 'Look up barcode' }).click();

  await expect(page).toHaveURL(new RegExp(`/scan\\?gtin=${UNKNOWN_GTIN}$`));
  await expect(page.getByRole('heading', { name: 'Add a soda' })).toBeVisible();
  // GTINs are normalized to 14 digits on ingress, so the EAN-13 gains a
  // leading zero in the form.
  await expect(page.locator('input[name="barcode"]')).toHaveValue(`0${UNKNOWN_GTIN}`);
  await expect(page.getByText(/something went wrong/i)).toHaveCount(0);

  const name = unique('E2E Unknown Fizz');
  await page.locator('input[name="name"]').fill(name);
  await page.locator('input[name="brand"]').fill('E2E Barcode Co');
  await page.getByRole('button', { name: 'Add to catalog' }).click();

  await expect(page).toHaveURL(/\/drinks\/.+\?created=1$/);
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
  await expect(page.getByText(/provisional/i).first()).toBeVisible();

  await page.getByRole('link', { name: 'Log this soda' }).click();
  await expect(page).toHaveURL(/\/checkins\/new\?slug=/);
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save check-in' })).toBeVisible();
});

test('manual add lands provisional, dedupes a repeat submit, and the result can be logged', async ({ page }) => {
  await registerAndVerify(page);

  const name = unique('E2E Dedupe Soda');
  const brand = 'E2E Dedupe Brand';

  // The empty search result is the documented entry point into the catalog.
  await page.goto(`/search?q=${encodeURIComponent(name)}`);
  await expect(page.getByText(/No soda matched/i)).toBeVisible();
  await page.getByRole('link', { name: 'Add it to the catalog' }).click();
  await expect(page).toHaveURL(/\/new/);

  await page.locator('input[name="name"]').fill(name);
  await page.locator('input[name="brand"]').fill(brand);
  await page.locator('input[name="volume_ml"]').fill('355');
  await page.locator('select[name="container"]').selectOption('can');
  await page.getByRole('button', { name: 'Add to catalog' }).click();

  await expect(page).toHaveURL(/\/drinks\/.+\?created=1$/);
  const createdURL = page.url();
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
  await expect(page.getByText(/provisional/i).first()).toBeVisible();

  // Re-submitting the same soft key must not create a duplicate.
  await page.goto('/new');
  await page.locator('input[name="name"]').fill(name);
  await page.locator('input[name="brand"]').fill(brand);
  await page.locator('input[name="volume_ml"]').fill('355');
  await page.locator('select[name="container"]').selectOption('can');
  await page.getByRole('button', { name: 'Add to catalog' }).click();

  await expect(page).toHaveURL(/\/drinks\/.+\?existing=1$/);
  expect(new URL(page.url()).pathname).toBe(new URL(createdURL).pathname);

  await page.goto(`/search?q=${encodeURIComponent(name)}`);
  const resultCard = page.locator('.result-card').filter({ hasText: name });
  await expect(resultCard).toHaveCount(1);

  await resultCard.getByRole('link', { name: 'Log' }).click();
  await expect(page).toHaveURL(/\/checkins\/new\?slug=/);
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save check-in' })).toBeVisible();
});

test('the composer tags a friend, captures a photo and creates a venue from a location fix', async ({ page, browser, request }) => {
  const alice = await registerAndVerify(page);

  const bobContext = await browser.newContext();
  const bobPage = await bobContext.newPage();
  const bob = await registerAndVerify(bobPage, newUser());
  await becomeFriends(request, page, bobPage, alice, bob);
  await bobContext.close();

  const drinkName = 'Coca-Cola';
  const venueName = unique('E2E Corner Fridge');
  const note = unique('E2E composer pour');

  await page.goto('/checkins/new?slug=coca-cola-355');
  await expect(page.getByRole('heading', { level: 1, name: drinkName })).toBeVisible();

  // Tag the friend through the visible picker.
  const friendRow = page.locator('label.friend-option').filter({ hasText: bob.handle });
  await expect(friendRow).toBeVisible();
  await friendRow.locator('input[name="friend_ids"]').check();
  await expect(friendRow).toHaveClass(/is-selected/);

  await page.locator('select[name="rating"]').selectOption('4.50');

  // Nearby is an explicit two-step flow: location first, provider lookup only
  // after the separate button. The consent copy is part of the contract.
  await page.getByRole('radio', { name: 'Nearby' }).check();
  await expect(page.locator('[data-where-panel="nearby"]')).toBeVisible();
  await expect(
    page.getByText(/We'll send your approximate location.*Photon \(OpenStreetMap\).*Sodapop does not store it\./),
  ).toBeVisible();

  // Attach the location from the click gesture.
  await page.getByRole('button', { name: 'Use my location' }).click();
  await expect(page.locator('[data-location-status]')).toContainText('Location attached');
  await expect(page.locator('input[name="lat"]')).toHaveValue(LAT.toFixed(3));
  await expect(page.locator('input[name="lng"]')).toHaveValue(LNG.toFixed(3));
  await expect(page.getByRole('button', { name: 'Show nearby places' })).toBeVisible();

  // Return to Search to create a venue from the same location fix.
  await page.getByRole('radio', { name: 'Search' }).check();
  await expect(page.locator('[data-where-panel="search"]')).toBeVisible();

  // Search for a venue that does not exist yet, then create it inline.
  await page.locator('#venue-q').fill(venueName);
  await page.locator('.checkin-venue').getByRole('button', { name: 'Search' }).click();
  await expect(page.locator('[data-venue-status]')).toContainText('No venue matched');

  await page.locator('.venue-create summary').click();
  await page.locator('input[name="new_venue_name"]').fill(venueName);
  await page.locator('input[name="new_venue_city"]').fill('Testville');
  await page.locator('input[name="new_venue_country"]').fill('NL');

  // Local preview appears before the upload.
  await page.locator('input[name="media"]').setInputFiles(PHOTO_FIXTURE);
  await expect(page.locator('[data-photo-preview] img')).toBeVisible();

  await page.locator('textarea[name="note"]').fill(note);
  await page.getByRole('button', { name: 'Save check-in' }).click();
  await expect(page).toHaveURL(/\/$/);

  const card = page.locator('.checkin').filter({ hasText: venueName }).first();
  await expect(card).toBeVisible();
  await expect(card).toContainText(note);
  await expect(card.locator(`a[href="/u/${bob.handle}"]`)).toBeVisible();
  const feedPhoto = card.locator('img.checkin-media');
  await expect(feedPhoto).toBeVisible();
  await expect.poll(async () => feedPhoto.evaluate((img) => (img as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);

  const checkinID = await card.getAttribute('data-checkin-id');
  expect(checkinID).toBeTruthy();

  await page.goto(`/checkins/${checkinID}`);
  await expect(page.locator('.detail-media')).toBeVisible();
  await expect.poll(async () => page.locator('.detail-media').evaluate((img) => (img as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  await expect(page.locator(`a[href="/u/${bob.handle}"]`)).toBeVisible();
  await expect(page.getByRole('link', { name: venueName })).toBeVisible();
  // The coordinate map link is intentionally only rendered when the pour has
  // no venue, so that branch lives in the location-only test below.
});

test('location-only and at-home check-ins expose no coordinates', async ({ page, request }) => {
  await registerAndVerify(page);
  const token = await createToken(page);
  const headers = { Authorization: `Bearer ${token}` };
  const requestFn = request;

  async function assertNoCoordsInPayload(id: string, label: string) {
    const res = await requestFn.get(`/api/v1/checkins/${id}`, { headers });
    expect(res.status(), label).toBe(200);
    const payload = await res.json();
    expect(payload.data, `${label} payload`).not.toHaveProperty('lat');
    expect(payload.data, `${label} payload`).not.toHaveProperty('lng');
    return payload.data as Record<string, unknown>;
  }

  // 1. A location-only pour is accepted, but the DOM and API never show the fix.
  const locationNote = unique('E2E map-only pour');
  await page.goto('/checkins/new?slug=coca-cola-355');
  await page.getByRole('radio', { name: 'Nearby' }).check();
  await page.getByRole('button', { name: 'Use my location' }).click();
  await expect(page.locator('[data-location-status]')).toContainText('Location attached');
  await page.getByRole('radio', { name: 'Search' }).check();
  await page.locator('textarea[name="note"]').fill(locationNote);
  await page.getByRole('button', { name: 'Save check-in' }).click();
  await expect(page).toHaveURL(/\/$/);

  const locationCard = page.locator('.checkin').filter({ hasText: locationNote }).first();
  await expect(locationCard).toBeVisible();
  await expect(locationCard.locator('a[href*="openstreetmap.org"]')).toHaveCount(0);
  await expect(page.getByText(LAT.toFixed(3), { exact: false })).toHaveCount(0);
  await expect(page.getByText(LNG.toFixed(3), { exact: false })).toHaveCount(0);

  const locationID = await locationCard.getAttribute('data-checkin-id');
  expect(locationID).toBeTruthy();
  const locationData = await assertNoCoordsInPayload(locationID!, 'location-only');
  expect(locationData.at_home).toBe(false);

  await page.goto(`/checkins/${locationID}`);
  await expect(page.locator('a[href*="openstreetmap.org"]')).toHaveCount(0);
  await expect(page.getByText(LAT.toFixed(3), { exact: false })).toHaveCount(0);
  await expect(page.getByText(LNG.toFixed(3), { exact: false })).toHaveCount(0);

  // 2. At home renders the shared @home pill and stores no coordinate.
  const homeNote = unique('E2E at-home pour');
  await page.goto('/checkins/new?slug=coca-cola-355');
  await page.getByRole('radio', { name: 'At home' }).check();
  await expect(page.locator('[data-where-panel="home"]')).toBeVisible();
  await expect(page.locator('input[name="at_home"]')).toHaveValue('1');
  await page.locator('textarea[name="note"]').fill(homeNote);
  await page.getByRole('button', { name: 'Save check-in' }).click();
  await expect(page).toHaveURL(/\/$/);

  const homeCard = page.locator('.checkin').filter({ hasText: homeNote }).first();
  await expect(homeCard).toBeVisible();
  await expect(homeCard.locator('.chip-at-home')).toHaveText('@home');
  await expect(homeCard.locator('a[href*="openstreetmap.org"]')).toHaveCount(0);

  const homeID = await homeCard.getAttribute('data-checkin-id');
  expect(homeID).toBeTruthy();
  const homeData = await assertNoCoordsInPayload(homeID!, 'at-home');
  expect(homeData.at_home).toBe(true);

  await page.goto(`/checkins/${homeID}`);
  await expect(page.getByText('@home').first()).toBeVisible();
  await expect(page.locator('a[href*="openstreetmap.org"]')).toHaveCount(0);

  // 3. A venue-attached pour shows the venue name, never a coordinate link.
  const search = await requestFn.get('/api/v1/search?q=cola', { headers });
  expect(search.status()).toBe(200);
  const drink = ((await search.json()).data as Array<{ id: number }>)[0];
  const venueName = unique('E2E Located Venue');
  // Offset the venue fixture so a prior spec's same-coordinate venue cannot
  // dedupe it (Create merges venues within 150 m).
  const venueLat = LAT + 0.02;
  const venueLng = LNG + 0.02;
  const venueRes = await requestFn.post('/api/v1/venues', {
    headers,
    data: { name: venueName, kind: 'shop', city: 'Testville', country: 'NL', lat: venueLat, lng: venueLng },
  });
  expect([200, 201]).toContain(venueRes.status());
  const venue = (await venueRes.json()).data as { id: number };
  const venueCheckinRes = await requestFn.post('/api/v1/checkins', {
    headers,
    data: {
      drink_id: drink.id,
      venue_id: venue.id,
      lat: LAT,
      lng: LNG,
      visibility: 'public',
      note: unique('E2E venue pour'),
    },
  });
  expect([200, 201]).toContain(venueCheckinRes.status());
  const venueCheckin = (await venueCheckinRes.json()).data as { id: string };
  await assertNoCoordsInPayload(venueCheckin.id, 'venue-attached');

  await page.goto(`/checkins/${venueCheckin.id}`);
  await expect(page.getByRole('link', { name: venueName })).toBeVisible();
  await expect(page.locator('a[href*="openstreetmap.org"]')).toHaveCount(0);
  await expect(page.getByText(LAT.toFixed(3), { exact: false })).toHaveCount(0);
  await expect(page.getByText(LNG.toFixed(3), { exact: false })).toHaveCount(0);
});
