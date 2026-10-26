import { readFileSync } from 'node:fs';
import { expect, test } from '@playwright/test';
import { createToken, registerAndVerify } from './helpers';

const version = (JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8')) as { version: string }).version;

test.describe('public surface', () => {
  test('landing page explains the product', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('A quiet place for loud fizz.');
    await expect(page.getByRole('link', { name: 'Create an account' })).toBeVisible();
    await expect(page.locator('footer')).toContainText(`Sodapop ${version}`);
  });

  test('swagger UI is published', async ({ page }) => {
    await page.goto('/api/docs/');
    await expect(page.locator('.swagger-ui').first()).toBeVisible();
    await expect(page.getByRole('heading', { name: /Sodapop API/ })).toBeVisible();
    const spec = await page.request.get('/api/openapi.json');
    expect(spec.ok()).toBeTruthy();
    const body = await spec.json();
    expect(body.openapi).toMatch(/^3\./);
  });

  test('anonymous API calls are refused', async ({ request }) => {
    const res = await request.get('/api/v1/feed');
    expect(res.status()).toBe(401);
    expect(res.headers()['www-authenticate']).toContain('Bearer');
  });
});

test.describe('soda journey', () => {
  test('register, verify, scan-search, and log a soda', async ({ page }) => {
    await registerAndVerify(page);

    await page.goto('/search?q=cola');
    await expect(page.getByRole('link', { name: /Coca-Cola/ }).first()).toBeVisible();
    await page.getByRole('link', { name: /Coca-Cola/ }).first().click();

    await expect(page.getByRole('heading', { level: 1 })).toContainText('Coca-Cola');
    await page.getByRole('link', { name: 'Log this soda' }).click();
    await page.getByLabel('Rating').selectOption('4.25');
    await page.getByLabel('Note').fill('Sharp, cold, and worth the detour.');
    await page.getByRole('button', { name: 'Save check-in' }).click();

    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByText('Coca-Cola').first()).toBeVisible();
    await expect(page.getByRole('button', { name: /Toast/ }).first()).toBeVisible();
  });

  test('a new drink can be added from search', async ({ page }) => {
    await registerAndVerify(page);
    await page.goto('/new');
    await page.getByLabel('Name').fill('E2E Cucumber Soda');
    await page.getByLabel('Brand').fill('E2E Works');
    await page.getByLabel('Style').selectOption('sparkling-water');
    await page.getByRole('button', { name: 'Add to catalog' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toContainText('E2E Cucumber Soda');
    await expect(page.getByText(/provisional/i).first()).toBeVisible();
  });
});

test.describe('API surface is friend-scoped', () => {
  test('token holder cannot enumerate the catalog or users', async ({ page, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);
    expect(token.startsWith('sd_')).toBeTruthy();
    const headers = { Authorization: `Bearer ${token}` };

    const me = await request.get('/api/v1/me', { headers });
    expect(me.ok()).toBeTruthy();

    for (const path of ['/api/v1/drinks', '/api/v1/users', '/api/v1/venues', '/api/v1/checkins']) {
      const res = await request.get(path, { headers });
      expect([404, 405], `${path} must not be a collection endpoint`).toContain(res.status());
    }

    const search = await request.get('/api/v1/search?q=cola&limit=100', { headers });
    expect(search.ok()).toBeTruthy();
    const payload = await search.json();
    expect(payload.data.length).toBeLessThanOrEqual(10);
    expect(JSON.stringify(payload)).not.toContain('"total"');
  });
});
