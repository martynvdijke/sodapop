import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

// Store reviewers and signed-out visitors both open these before they have an
// account, so they must render without a session and stay accessible.
const pages = [
  { path: '/privacy', heading: 'Privacy' },
  { path: '/terms', heading: 'Terms of use' },
  { path: '/account/delete', heading: 'Delete your account' },
];

async function expectNoSeriousViolations(page: Page, path: string) {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(serious.map((v) => `${v.id}: ${v.help}`), `serious violations on ${path}`).toEqual([]);
}

test.describe('legal pages', () => {
  for (const { path, heading } of pages) {
    test(`${path} renders signed out and passes axe`, async ({ page }) => {
      const response = await page.goto(path);
      expect(response?.status()).toBe(200);
      await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
      await expectNoSeriousViolations(page, path);
    });
  }

  test('the footer links to every legal page', async ({ page }) => {
    await page.goto('/privacy');
    const footer = page.locator('footer');
    await expect(footer.getByRole('link', { name: 'Privacy', exact: true })).toHaveAttribute('href', '/privacy');
    await expect(footer.getByRole('link', { name: 'Terms', exact: true })).toHaveAttribute('href', '/terms');
    await expect(footer.getByRole('link', { name: 'Delete your account' })).toHaveAttribute('href', '/account/delete');
  });

  test('the privacy policy describes the session cookie and the data export', async ({ page }) => {
    await page.goto('/privacy');
    await expect(page.locator('#cookies')).toContainText('Cookies and tracking');
    await expect(page.locator('section.legal')).toContainText('sd_session');
    await expect(page.getByRole('link', { name: 'Download your data' })).toHaveAttribute('href', '/export.json');
  });
});
