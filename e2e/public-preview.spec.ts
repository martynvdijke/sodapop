import { expect, test } from '@playwright/test';

test.describe('public preview', () => {
  test('anonymous visitors see public pours and people', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Public pours' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Public people' })).toBeVisible();
    await expect(page.getByText('Public pours can be seen by anyone')).toBeVisible();

    await page.getByRole('link', { name: /Public people/ }).click();
    await expect(page.getByRole('heading', { name: 'Public people' })).toBeVisible();
    await expect(page.getByRole('link', { name: /Demo Soda/ })).toBeVisible();
  });

  test('public check-in detail is read-only', async ({ page }) => {
    await page.goto('/');
    const firstPour = page.getByRole('link', { name: 'View pour' }).first();
    await expect(firstPour).toBeVisible();
    await firstPour.click();
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(page.getByRole('button', { name: /toast/i })).toHaveCount(0);
    await expect(page.locator('form[action$="/toast"]')).toHaveCount(0);
  });

  test('anonymous writes bounce to login', async ({ page }) => {
    await page.goto('/checkins/new');
    await expect(page).toHaveURL(/\/login\?next=/);
    await expect(page.getByRole('heading', { name: /Sign in/ })).toBeVisible();
  });

  test('demo account signs in and can post', async ({ page }) => {
    await page.goto('/login');
    await expect(page.getByText('Shared account')).toContainText('@demo');
    await page.getByRole('button', { name: 'Use the demo account' }).click();
    await expect(page).not.toHaveURL(/\/login/);
    await page.goto('/checkins/new?slug=coca-cola-355');
    await expect(page.getByRole('heading', { name: 'Coca-Cola' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Save check-in' })).toBeVisible();
  });
});
