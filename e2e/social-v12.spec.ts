import { expect, test } from '@playwright/test';
import { createToken, registerAndVerify } from './helpers';

const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP49OXZfwAJdAPMUnIAlQAAAABJRU5ErkJggg==',
  'base64',
);

test.describe('v1.2 comments, compare, and venue media', () => {
  test('a member can comment on a check-in and delete their comment', async ({ page, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);
    const headers = { Authorization: `Bearer ${token}` };

    const search = await request.get('/api/v1/search?q=cola', { headers });
    const drink = (await search.json()).data[0] as { id: number };
    const created = await request.post('/api/v1/checkins', {
      headers,
      data: { drink_id: drink.id, rating: 4.5, serving: 'can', visibility: 'public', note: 'comment target' },
    });
    const checkin = (await created.json()).data as { id: string };

    await page.goto(`/checkins/${checkin.id}`);
    await expect(page.getByRole('heading', { name: 'Comments', exact: true })).toBeVisible();

    const body = 'First sip is the best sip.';
    await page.getByLabel('Add a comment').fill(body);
    await page.getByRole('button', { name: 'Post comment' }).click();

    await expect(page.getByText(body)).toBeVisible();
    await expect(page.getByRole('heading', { name: '1 comment' })).toBeVisible();

    const comment = page.locator('.comment').filter({ hasText: body });
    await comment.getByRole('button', { name: 'Delete' }).click();

    await expect(page.getByText(body)).toBeHidden();
    await expect(page.getByText('No comments yet.')).toBeVisible();
  });

  test('compare renders a table and rejects invalid selections', async ({ page }) => {
    await registerAndVerify(page);

    await page.goto('/compare?drinks=coca-cola-355,fentimans-curiosity-cola-275');
    await expect(page.getByRole('heading', { name: 'Compare sodas' })).toBeVisible();
    await expect(page.getByRole('table')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Coca-Cola' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Fentimans Curiosity Cola' })).toBeVisible();
    await expect(page.getByRole('rowheader', { name: 'Sugar' })).toBeVisible();

    await page.goto('/compare?drinks=coca-cola-355');
    await expect(page.getByText('Choose between 2 and 6 sodas to compare.')).toBeVisible();
    await expect(page.getByRole('table')).toHaveCount(0);

    await page.goto('/compare?drinks=coca-cola-355,coca-cola-355');
    await expect(page.getByText('Each soda can only be compared once.')).toBeVisible();

    const unknown = await page.goto('/compare?drinks=coca-cola-355,not-a-real-soda');
    expect(unknown?.status()).toBe(404);
  });

  test('a member can upload venue media and report it', async ({ page, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);
    const headers = { Authorization: `Bearer ${token}` };

    const name = `E2E Fizz Hall ${Date.now()}`;
    const venueResponse = await request.post('/api/v1/venues', {
      headers,
      data: { name, kind: 'shop', city: 'Testville', country: 'NL' },
    });
    expect(venueResponse.ok()).toBeTruthy();
    const venue = (await venueResponse.json()).data as { id: number };

    await page.goto(`/venues/${venue.id}`);
    await expect(page.getByRole('heading', { level: 1 })).toContainText(name);

    await page.locator('input[name="image"]').setInputFiles({
      name: 'counter.png',
      mimeType: 'image/png',
      buffer: png,
    });
    await page.getByLabel('Kind').selectOption('photo');
    await page.getByLabel('Label').fill('E2E counter');
    await page.getByRole('button', { name: 'Upload' }).click();

    await expect(page.getByText('Photo · E2E counter')).toBeVisible();
    await expect(page.getByRole('img', { name: /Photo: E2E counter/ })).toBeVisible();

    await page.getByText('Report', { exact: true }).click();
    await page.getByLabel('Reason').selectOption('incorrect');
    await page.getByRole('button', { name: 'Send report' }).click();

    await expect(page.getByText('Photo · E2E counter')).toBeVisible();
  });
});
