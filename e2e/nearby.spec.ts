import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext } from '@playwright/test';
import { createToken, registerAndVerify } from './helpers';

const LAT = 51.5;
const LNG = -0.12;

test.use({
  geolocation: { latitude: LAT, longitude: LNG },
  permissions: ['geolocation'],
});

function auth(token: string) {
  return { Authorization: `Bearer ${token}` };
}

async function createVenue(request: APIRequestContext, token: string, name: string) {
  const res = await request.post('/api/v1/venues', {
    headers: auth(token),
    data: {
      name,
      kind: 'shop',
      city: 'Testville',
      country: 'NL',
      lat: LAT,
      lng: LNG,
    },
  });
  expect([200, 201]).toContain(res.status());
  return (await res.json()).data as { id: number };
}

async function createCheckin(
  request: APIRequestContext,
  token: string,
  venueID: number,
  note: string,
) {
  const search = await request.get('/api/v1/search?q=cola', { headers: auth(token) });
  expect(search.status()).toBe(200);
  const drinks = (await search.json()).data as Array<{ id: number }>;
  expect(drinks.length).toBeGreaterThan(0);

  const res = await request.post('/api/v1/checkins', {
    headers: auth(token),
    data: {
      drink_id: drinks[0].id,
      venue_id: venueID,
      rating: 4.25,
      serving: 'can',
      visibility: 'public',
      note,
    },
  });
  const body = await res.text();
  expect([200, 201], body).toContain(res.status());
  return JSON.parse(body).data as { id: string };
}

test('nearby feed finds a visible pour and never exposes coordinates', async ({ page }) => {
  const user = await registerAndVerify(page);
  const token = await createToken(page);
  const suffix = Date.now().toString(36);
  const venueName = `E2E Nearby Fridge ${suffix}`;
  const note = `e2e nearby pour ${suffix}`;

  const venue = await createVenue(page.request, token, venueName);
  await createCheckin(page.request, token, venue.id, note);

  await page.goto('/nearby');

  // Without coordinates the search is refused in-page, not with a redirect.
  await page.getByRole('button', { name: 'Show nearby pours' }).click();
  await expect(
    page.getByText('Set your location first, then show nearby pours.', { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Use my location' })).toBeFocused();

  await page.getByRole('button', { name: 'Use my location' }).click();
  await expect(page.getByText('Location ready', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Show nearby pours' }).click();
  await expect(page.getByRole('heading', { name: 'Nearby pours' })).toBeVisible();
  await expect(page.getByText(note, { exact: true })).toBeVisible();
  await expect(page.getByText('Within 100 m', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: venueName, exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: `@${user.handle}`, exact: true })).toBeVisible();

  // Coordinates are only ever held in the hidden fields for this request.
  expect(page.url()).not.toContain('lat=');
  expect(page.url()).not.toContain('lng=');
  await expect(page.locator('input[name="lat"]')).toHaveValue(LAT.toFixed(3));
  await expect(page.locator('input[name="lng"]')).toHaveValue(LNG.toFixed(3));

  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(serious.map((v) => `${v.id}: ${v.help}`)).toEqual([]);
});

test('nearby search requires a signed-in viewer', async ({ page }) => {
  await page.goto('/nearby');
  await expect(page).toHaveURL(/\/login/);
});
