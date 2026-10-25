import { expect, test } from '@playwright/test';
import { createToken, newUser, registerAndVerify, signIn } from './helpers';

test.describe('v1.1 member surfaces', () => {
  test('a friend request lands in notifications and can be marked read', async ({ page, browser }) => {
    const alice = await registerAndVerify(page);

    const otherContext = await browser.newContext();
    const bobPage = await otherContext.newPage();
    await registerAndVerify(bobPage);
    await bobPage.goto('/friends');
    await bobPage.getByLabel('Friend handle').fill(alice.handle);
    await bobPage.getByRole('button', { name: 'Send request' }).click();
    await otherContext.close();

    await page.goto('/notifications');
    await expect(page.getByRole('heading', { name: 'Notifications' })).toBeVisible();
    await expect(page.getByText(/wants to be friends/i)).toBeVisible();
    await expect(page.locator('.nav-badge')).toBeVisible();

    await page.getByRole('button', { name: 'Mark all read' }).click();
    await expect(page.locator('.nav-badge')).toBeHidden();
  });

  test('a check-in has a detail page, can be toasted and deleted by its owner', async ({ page, request, browser }) => {
    await registerAndVerify(page);
    const token = await createToken(page);

    const search = await request.get('/api/v1/search?q=cola', { headers: { Authorization: `Bearer ${token}` } });
    const drink = (await search.json()).data[0] as { id: number; slug: string; name: string };
    const created = await request.post('/api/v1/checkins', {
      headers: { Authorization: `Bearer ${token}` },
      data: { drink_id: drink.id, rating: 4.5, serving: 'can', visibility: 'public', note: 'crisp and cold' },
    });
    const checkin = (await created.json()).data as { id: string };

    await page.goto(`/checkins/${checkin.id}`);
    await expect(page.getByRole('heading', { level: 1 })).toContainText(drink.name);
    await expect(page.getByText('crisp and cold')).toBeVisible();

    // Owners cannot toast their own pour.
    await expect(page.getByRole('button', { name: 'Toast' })).toHaveCount(0);

    const friendContext = await browser.newContext();
    const friendPage = await friendContext.newPage();
    await registerAndVerify(friendPage);
    await friendPage.goto(`/checkins/${checkin.id}`);
    await friendPage.getByRole('button', { name: 'Toast' }).click();
    await expect(friendPage.getByRole('button', { name: 'Toasted' })).toBeDisabled();
    await friendContext.close();

    await page.reload();
    await expect(page.getByText(/1 toast/)).toBeVisible();

    await page.getByRole('button', { name: 'Delete check-in' }).click();
    await expect(page).toHaveURL(/\/you$/);
    const gone = await page.goto(`/checkins/${checkin.id}`);
    expect(gone?.status()).toBe(404);
  });

  test('settings save the profile and an opt-in profile becomes anonymously readable and indexable', async ({ page, browser }) => {
    const user = await registerAndVerify(page);

    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: 'Your account', exact: true })).toBeVisible();
    await page.getByLabel('Display name').fill('E2E Pat');
    await page.getByRole('button', { name: 'Save profile' }).click();
    await page.goto('/settings');
    await expect(page.getByLabel('Display name')).toHaveValue('E2E Pat');

    await page.goto('/settings');
    await page.locator('input[name="public_profile"]').check();
    await page.getByRole('button', { name: 'Save privacy settings' }).click();

    const anonymous = await browser.newContext();
    const anonPage = await anonymous.newPage();
    const response = await anonPage.goto(`/u/${user.handle}`);
    expect(response?.status()).toBe(200);
    await expect(anonPage.getByRole('heading', { level: 1 })).toContainText('E2E Pat');
    expect(response?.headers()['x-robots-tag']).toBeUndefined();
    await anonymous.close();

    await page.goto('/settings');
    await page.locator('input[name="public_profile"]').uncheck();
    await page.getByRole('button', { name: 'Save privacy settings' }).click();
    const privateContext = await browser.newContext();
    const privatePage = await privateContext.newPage();
    const privateResponse = await privatePage.goto(`/u/${user.handle}`);
    expect(privatePage.url()).toContain('/login');
    expect(privateResponse?.headers()['x-robots-tag']).toContain('noindex');
    await privateContext.close();
  });

  test('the scanner resolves a manual barcode into a check-in', async ({ page }) => {
    await registerAndVerify(page);
    await page.goto('/scan');
    await expect(page.locator('[data-scanner]')).toHaveCount(1);
    expect(await page.locator('[data-scanner-open]').count()).toBeGreaterThan(0);

    await page.goto('/scan?gtin=5449000000996');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Coca-Cola');
    await expect(page.getByRole('button', { name: 'Save check-in' })).toBeVisible();
  });

  test('insights add up a pour and password reset stays anti-enumerating', async ({ page, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);
    const search = await request.get('/api/v1/search?q=cola', { headers: { Authorization: `Bearer ${token}` } });
    const drink = (await search.json()).data[0] as { id: number };
    await request.post('/api/v1/checkins', {
      headers: { Authorization: `Bearer ${token}` },
      data: { drink_id: drink.id, serving: 'can', visibility: 'public' },
    });

    await page.goto('/insights');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('What your sodas add up to');
    await expect(page.getByRole('heading', { name: 'Sugar' })).toBeVisible();

    await page.goto('/forgot');
    await expect(page.getByRole('heading', { name: 'Reset your password' })).toBeVisible();
    const unknown = newUser();
    await page.getByLabel('Email').fill(unknown.email);
    await page.getByRole('button', { name: 'Send reset link' }).click();
    await expect(page.getByRole('heading', { name: 'Reset your password' })).toBeVisible();
    await expect(page.getByText(unknown.email)).toBeHidden();
  });

  test('the seeded admin can move a drink through its lifecycle', async ({ page }) => {
    await signIn(page, { handle: 'e2e_admin', email: 'e2e_admin@example.com', password: 'correct-horse-battery' });
    await page.goto('/drinks/coca-cola-355');
    await page.getByText('Lifecycle').click();
    await page.getByLabel('Status').selectOption('discontinued');
    await page.getByRole('button', { name: /save|update/i }).click();
    await expect(page.getByText('Discontinued').first()).toBeVisible();

    await page.getByText('Lifecycle').click();
    await page.getByLabel('Status').selectOption('active');
    await page.getByRole('button', { name: /save|update/i }).click();
    await expect(page.getByText('Active').first()).toBeVisible();
  });
});
