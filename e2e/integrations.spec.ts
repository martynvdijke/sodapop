import { expect, test } from '@playwright/test';
import { registerAndVerify } from './helpers';

test.describe('Home Assistant integration', () => {
  test('mints an inbound token, replays a webhook, then revokes it', async ({ page, request }) => {
    await registerAndVerify(page);

    await page.goto('/integrations');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(page.getByText('No tokens yet', { exact: false })).toBeVisible();

    const label = `E2E inbound ${Date.now().toString(36)}`;
    await page.getByLabel('Label').fill(label);
    await page.getByRole('button', { name: 'Create token' }).click();

    const reveal = page.locator('.token-box');
    await expect(reveal).toBeVisible();
    const rawToken = (await reveal.locator('code').first().textContent())?.trim() ?? '';
    expect(rawToken).not.toBe('');

    // The token list gains a row for the new label.
    await expect(page.getByText(label, { exact: true })).toBeVisible();
    await expect(page.getByText('No tokens yet', { exact: false })).toHaveCount(0);

    const idempotencyKey = `e2e-ha-${Date.now().toString(36)}`;
    const post = () =>
      request.post(`/hooks/ha/${rawToken}`, {
        data: { slug: 'coca-cola-355', rating: 4 },
        headers: { 'Idempotency-Key': idempotencyKey },
      });

    const first = await post();
    expect(first.status()).toBe(201);
    const firstBody = await first.json();
    expect(firstBody.data.checkin_id).toBeTruthy();

    const replay = await post();
    expect(replay.status()).toBe(200);
    const replayBody = await replay.json();
    expect(replayBody.data.checkin_id).toBe(firstBody.data.checkin_id);

    await page.getByRole('button', { name: `Revoke the token labelled ${label}` }).click();
    await expect(page.getByText('Revoked', { exact: true })).toBeVisible();

    const afterRevoke = await post();
    expect(afterRevoke.status()).toBe(401);
  });
});
