import { expect, test } from '@playwright/test';
import { registerAndVerify, signIn } from './helpers';

const admin = { handle: 'e2e_admin', email: 'e2e_admin@example.com', password: 'correct-horse-battery' };

test.describe.configure({ mode: 'serial' });

test.describe('moderation queue', () => {
  test('a member reports a profile and an admin suspends the account', async ({ page, browser }) => {
    const target = await registerAndVerify(page);

    // A public profile is needed before a stranger can reach it at all.
    await page.goto('/settings');
    await page.getByRole('checkbox', { name: /Public profile/ }).check();
    const saved = page.waitForResponse(
      (res) => res.request().method() === 'POST' && res.url().endsWith('/settings/privacy'),
    );
    await page.getByRole('button', { name: 'Save privacy settings' }).click();
    await saved;

    const reporterContext = await browser.newContext();
    const reporter = await reporterContext.newPage();
    await registerAndVerify(reporter);

    await reporter.goto(`/u/${target.handle}`);
    await reporter.locator('details.profile-report summary').click();
    await reporter.locator('select[name="reason"]').selectOption('other');
    await reporter.locator('textarea[name="details"]').fill('Pretending to be someone else');
    await reporter.getByRole('button', { name: 'Send report' }).click();
    await expect(reporter).toHaveURL(new RegExp(`/u/${target.handle}$`));

    const adminContext = await browser.newContext();
    const moderator = await adminContext.newPage();
    await signIn(moderator, admin);
    await moderator.goto('/admin');

    const entry = moderator.locator('article.card').filter({ hasText: target.handle });
    await expect(entry).toContainText('Pretending to be someone else');
    await expect(entry.getByRole('button', { name: 'Resolve' })).toBeVisible();

    moderator.once('dialog', (dialog) => dialog.accept());
    await entry.getByRole('button', { name: 'Suspend account' }).click();

    await expect(moderator).toHaveURL(/\/admin\?done=suspended$/);
    await expect(moderator.getByText('Account suspended.')).toBeVisible();
    await expect(moderator.locator('article.card').filter({ hasText: target.handle })).toHaveCount(0);

    // Suspension is not cosmetic: the target's session stops working at once.
    await page.goto('/settings');
    await expect(page).toHaveURL(/\/login/);
  });
});
