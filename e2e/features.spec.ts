import { expect, test } from '@playwright/test';
import { registerAndVerify } from './helpers';

test.describe('v1 feature pages', () => {
  test('new surfaces render for a verified member', async ({ page }) => {
    await registerAndVerify(page);

    for (const [path, heading] of [
      ['/you', 'You'],
      ['/lists', 'Lists'],
      ['/passport', 'Soda passport'],
      ['/taste', 'Taste twin'],
      ['/mix', 'Mix lab'],
      ['/integrations', 'Integrations'],
      ['/venues', 'Venues'],
    ] as const) {
      const response = await page.goto(path);
      expect(response?.status(), `${path} should render`).toBe(200);
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    }
  });

  test('a member can create a list and save a drink', async ({ page }) => {
    await registerAndVerify(page);

    await page.goto('/lists');
    await page.getByLabel('Name').fill('Friday fridge');
    await page.getByRole('button', { name: /create list/i }).click();
    await expect(page.getByText('Friday fridge')).toBeVisible();

    await page.goto('/drinks/coca-cola-355');
    await page.getByLabel('Save to list').selectOption({ label: 'Friday fridge' });
    await page.getByRole('button', { name: 'Save to list' }).click();
    await expect(page).toHaveURL(/\/lists\//);
    await expect(page.getByRole('link', { name: 'Coca-Cola' })).toBeVisible();
  });
});
