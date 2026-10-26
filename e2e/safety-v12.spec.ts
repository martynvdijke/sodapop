import { expect, test } from '@playwright/test';
import { createToken, registerAndVerify } from './helpers';

const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP49OXZfwAJdAPMUnIAlQAAAABJRU5ErkJggg==',
  'base64',
);

test.describe('v1.2 blocks and drink galleries', () => {
  test('blocking hides a public profile and unblocking restores it', async ({ page, browser }) => {
    const alice = await registerAndVerify(page);

    // Opt in to a public profile so a signed-in stranger can reach it before blocking.
    await page.goto('/settings');
    await page.getByRole('checkbox', { name: /Public profile/ }).check();
    const saved = page.waitForResponse(
      (res) => res.request().method() === 'POST' && res.url().endsWith('/settings/privacy'),
    );
    await page.getByRole('button', { name: 'Save privacy settings' }).click();
    await saved;
    await expect(page.getByRole('checkbox', { name: /Public profile/ })).toBeChecked();

    const context = await browser.newContext();
    const bob = await context.newPage();
    await registerAndVerify(bob);

    await bob.goto(`/u/${alice.handle}`);
    await expect(bob.locator('.profile-handle-text')).toContainText(`@${alice.handle}`);
    await bob.locator('details.profile-block summary').click();
    await bob.getByRole('button', { name: `Block @${alice.handle}` }).click();

    await expect(bob).toHaveURL(/\/settings\/blocked$/);
    await expect(bob.getByRole('heading', { name: 'Blocked people' })).toBeVisible();
    await expect(bob.locator('.blocked-row')).toContainText(`@${alice.handle}`);

    const blocked = await bob.goto(`/u/${alice.handle}`);
    expect(blocked?.status()).toBe(404);

    await bob.goto('/settings/blocked');
    await bob.getByRole('button', { name: `Unblock @${alice.handle}` }).click();
    await expect(bob.getByText("You haven't blocked anyone.")).toBeVisible();

    const restored = await bob.goto(`/u/${alice.handle}`);
    expect(restored?.status()).toBe(200);
    await expect(bob.locator('.profile-handle-text')).toContainText(`@${alice.handle}`);

    await context.close();
  });

  test('a member can add a credited photo to a drink gallery', async ({ page, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);
    const headers = { Authorization: `Bearer ${token}` };

    const search = await request.get('/api/v1/search?q=cola', { headers });
    const drink = (await search.json()).data[0] as { slug: string; name: string };

    await page.goto(`/drinks/${drink.slug}`);
    await expect(page.getByRole('heading', { name: 'Photos' })).toBeVisible();

    await page.locator('input[name="image"]').setInputFiles({
      name: 'gallery.png',
      mimeType: 'image/png',
      buffer: png,
    });
    await page.getByLabel('Credit').fill('E2E gallery');
    await page.getByRole('button', { name: 'Upload photo' }).click();

    const photo = page.locator('.gallery-item').filter({ hasText: 'E2E gallery' });
    await expect(photo).toBeVisible();
    await expect(photo.getByRole('img')).toHaveAttribute('alt', new RegExp(`Photo of ${drink.name}.*E2E gallery`));
    await expect(photo.getByText('E2E gallery')).toBeVisible();
  });
});
