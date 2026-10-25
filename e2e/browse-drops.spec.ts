import { expect, test } from '@playwright/test';
import { registerAndVerify } from './helpers';

test.describe('catalog browse', () => {
  test('filters drinks and reflects the query in the URL', async ({ page }) => {
    await registerAndVerify(page);

    await page.goto('/drinks');
    await expect(page.getByRole('heading', { level: 1, name: 'Drinks' })).toBeVisible();
    await expect(page.getByRole('link', { name: /^Coca-Cola\b/ })).toBeVisible();

    await page.getByLabel('Brand').fill('fentimans');
    await page.getByLabel('Sort').selectOption('rated');
    await page.getByRole('button', { name: 'Apply filters' }).click();

    await expect(page).toHaveURL(/\/drinks\?.*\bbrand=fentimans\b/);
    await expect(page).toHaveURL(/[?&]sort=rated\b/);

    // Only Fentimans drinks survive the brand filter.
    await expect(page.getByRole('link', { name: /^Fentimans Curiosity Cola/ })).toBeVisible();
    await expect(page.getByRole('link', { name: /^Fever-Tree Ginger Beer/ })).toBeVisible();
    await expect(page.getByRole('link', { name: /^Coca-Cola\b/ })).toHaveCount(0);
  });

  test('a seeded brand lists its drinks', async ({ page }) => {
    await registerAndVerify(page);

    await page.goto('/brands');
    await expect(page.getByRole('heading', { level: 1, name: 'Brands' })).toBeVisible();

    const brandLink = page.getByRole('link', { name: /Fentimans/ });
    await expect(brandLink).toBeVisible();
    await brandLink.click();

    await expect(page).toHaveURL(/\/brands\/fentimans$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Fentimans' })).toBeVisible();
    await expect(page.getByRole('link', { name: /^Fentimans Curiosity Cola/ })).toBeVisible();
    await expect(page.getByRole('link', { name: /^Fever-Tree Ginger Beer/ })).toBeVisible();
  });

  test('a seeded style lists its drinks', async ({ page }) => {
    await registerAndVerify(page);

    await page.goto('/styles');
    await expect(page.getByRole('heading', { level: 1, name: 'Styles' })).toBeVisible();

    const styleLink = page.getByRole('link', { name: 'Cola', exact: true });
    await expect(styleLink).toBeVisible();
    await styleLink.click();

    await expect(page).toHaveURL(/\/styles\/cola$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Cola' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Sodas' })).toBeVisible();
    await expect(page.getByRole('link', { name: /^Coca-Cola\b/ })).toBeVisible();
    await expect(page.getByRole('link', { name: /^Fentimans Curiosity Cola/ })).toBeVisible();
  });

  test('drops renders the limited, seasonal and discontinued sections', async ({ page }) => {
    await registerAndVerify(page);

    await page.goto('/drops');
    await expect(page.getByRole('heading', { level: 1, name: 'Drops' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Limited' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Seasonal' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Discontinued' })).toBeVisible();
  });
});

test.describe('catalog access control', () => {
  test('anonymous catalogue browsing redirects to login with noindex', async ({ page }) => {
    await page.goto('/drinks');
    await expect(page).toHaveURL(/\/login/);

    const response = await page.request.get('/drinks', { maxRedirects: 0 });
    expect(response.status()).toBe(302);
    expect(response.headers()['x-robots-tag'] ?? '').toContain('noindex');
  });
});
