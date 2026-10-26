import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { registerAndVerify } from './helpers';

const publicPages = ['/', '/login', '/register'];

for (const path of publicPages) {
  test(`no serious accessibility violations on ${path}`, async ({ page }) => {
    await page.goto(path);
    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze();
    const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
    expect(serious.map((v) => `${v.id}: ${v.help}`)).toEqual([]);
  });
}

test('no serious accessibility violations on the signed-in search page', async ({ page }) => {
  await registerAndVerify(page);
  await page.goto('/search?q=cola');
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(serious.map((v) => `${v.id}: ${v.help}`)).toEqual([]);
});

async function expectNoSeriousViolations(page: Page, path: string) {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(serious.map((v) => `${v.id}: ${v.help}`), `serious violations on ${path}`).toEqual([]);
}

test('no serious accessibility violations across signed-in member pages', async ({ page }) => {
  const user = await registerAndVerify(page);

  // Opt in to a public profile so /u/{handle} renders for the scan.
  await page.goto('/settings');
  await page.getByRole('checkbox', { name: /Public profile/ }).check();
  const saved = page.waitForResponse(
    (res) => res.request().method() === 'POST' && res.url().endsWith('/settings/privacy'),
  );
  await page.getByRole('button', { name: 'Save privacy settings' }).click();
  await saved;
  await page.waitForLoadState('load');
  await expect(page.getByRole('checkbox', { name: /Public profile/ })).toBeChecked();

  for (const path of [
    '/settings',
    '/settings/blocked',
    '/notifications',
    '/insights',
    '/drinks',
    '/drops',
    '/compare?drinks=coca-cola-355,fentimans-curiosity-cola-275',
    `/u/${user.handle}`,
  ]) {
    await page.goto(path);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expectNoSeriousViolations(page, path);
  }
});
