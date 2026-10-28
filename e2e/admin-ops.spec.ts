import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { signIn } from './helpers';

const admin = { handle: 'e2e_admin', email: 'e2e_admin@example.com', password: 'correct-horse-battery' };

test.describe.configure({ mode: 'serial' });

async function expectNoSeriousViolations(page: Page, path: string) {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(serious.map((v) => `${v.id}: ${v.help}`), `serious violations on ${path}`).toEqual([]);
}

test.describe('admin operations', () => {
  test('the admin nav reaches every ops page', async ({ page }) => {
    await signIn(page, admin);

    const overview = await page.goto('/admin');
    expect(overview?.status()).toBe(200);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

    const nav = page.getByRole('navigation', { name: 'Admin sections' });
    await expect(nav).toBeVisible();
    for (const label of ['Overview', 'Settings', 'Badges', 'Venues', 'Digest', 'System']) {
      await expect(nav.getByRole('link', { name: label, exact: true })).toBeVisible();
    }

    await nav.getByRole('link', { name: 'System', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/system$/);
    await expect(page.getByRole('heading', { level: 1, name: 'System' })).toBeVisible();
    await expectNoSeriousViolations(page, '/admin/system');

    await page.getByRole('navigation', { name: 'Admin sections' }).getByRole('link', { name: 'Badges', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/badges$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Badges' })).toBeVisible();
    await expectNoSeriousViolations(page, '/admin/badges');

    await page.getByRole('navigation', { name: 'Admin sections' }).getByRole('link', { name: 'Venues', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/venues$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Venues' })).toBeVisible();

    // No venues are part of the demo seed, so exercise the search empty state
    // and confirm the merge form still renders. Never submit the merge.
    await page.getByLabel('Search venues').fill('zzzz-no-such-venue-e2e');
    await page.getByRole('button', { name: 'Search' }).click();
    await expect(page).toHaveURL(/\/admin\/venues\?q=zzzz-no-such-venue-e2e$/);
    await expect(page.getByText(/No venues match/)).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Merge venues' })).toBeVisible();
    await expect(page.getByLabel('Source venue ID')).toBeVisible();
    await expect(page.getByLabel('Target venue ID')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Merge venues' })).toBeVisible();
    await expectNoSeriousViolations(page, '/admin/venues');

    await page.getByRole('navigation', { name: 'Admin sections' }).getByRole('link', { name: 'Digest', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/digest$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Weekly digest' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Send weekly digest now' })).toBeVisible();
    await expectNoSeriousViolations(page, '/admin/digest');
  });

  test('an admin can create a backup and manage a custom badge', async ({ page }) => {
    await signIn(page, admin);

    // Backups.
    await page.goto('/admin/system');
    await expect(page.getByRole('heading', { level: 2, name: 'Backups' })).toBeVisible();
    await page.getByRole('button', { name: 'Create backup' }).click();
    await expect(page).toHaveURL(/\/admin\/system\?backup=ok$/);
    await expect(page.getByText('Backup created.')).toBeVisible();
    await expect(page.getByText(/sodapop-\d{8}-\d{6}[^/]*\.db/).first()).toBeVisible();

    // Custom badge lifecycle.
    const suffix = Date.now().toString(36);
    const slug = `e2e-badge-${suffix}`;
    const name = `E2E Badge ${suffix}`;
    await page.goto('/admin/badges');
    await expect(page.getByRole('heading', { level: 2, name: 'New badge' })).toBeVisible();

    const createForm = page.locator('form[action="/admin/badges"]');
    await createForm.getByLabel('Name', { exact: true }).fill(name);
    await createForm.getByLabel('Slug', { exact: true }).fill(slug);
    await createForm.getByLabel('Kind').selectOption('local');
    await createForm.locator('#badge-create-rule').selectOption('checkins');
    await createForm.getByLabel('Description').fill('Temporary badge created by the admin operations e2e test.');
    await createForm.locator('#badge-create-need').fill('10');

    await page.getByRole('button', { name: 'Create badge' }).click();
    await expect(page).toHaveURL(/\/admin\/badges\?created=1$/);
    await expect(page.getByText('Badge created.')).toBeVisible();

    const createdRow = page.locator('tr.badge-summary-row').filter({ hasText: slug });
    await expect(createdRow).toBeVisible();
    await expect(createdRow).toContainText(name);

    page.once('dialog', (dialog) => dialog.accept());
    await createdRow.getByRole('button', { name: 'Delete' }).click();
    await expect(page).toHaveURL(/\/admin\/badges\?deleted=1$/);
    await expect(page.getByText('Badge deleted.')).toBeVisible();
    await expect(page.locator('tr.badge-summary-row').filter({ hasText: slug })).toHaveCount(0);

    await expectNoSeriousViolations(page, '/admin/badges');
  });
});
