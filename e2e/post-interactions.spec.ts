import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Browser, type Page } from '@playwright/test';
import { createToken, registerAndVerify, type TestUser } from './helpers';

async function expectNoSeriousViolations(page: Page) {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(serious.map((v) => `${v.id}: ${v.help}`)).toEqual([]);
}

interface Member {
  context: Awaited<ReturnType<Browser['newContext']>>;
  page: Page;
  user: TestUser;
  headers: { Authorization: string };
}

/** Registers a second member in a separate browser context and returns an API token. */
async function secondMember(browser: Browser): Promise<Member> {
  const context = await browser.newContext();
  const page = await context.newPage();
  const user = await registerAndVerify(page);
  const token = await createToken(page);
  return { context, page, user, headers: { Authorization: `Bearer ${token}` } };
}

async function makeCheckin(
  request: APIRequestContext,
  headers: { Authorization: string },
  note: string,
): Promise<{ id: string }> {
  const search = await request.get('/api/v1/search?q=cola', { headers });
  const drink = (await search.json()).data[0] as { id: number };
  const created = await request.post('/api/v1/checkins', {
    headers,
    data: { drink_id: drink.id, rating: 4.5, serving: 'can', visibility: 'public', note },
  });
  expect(created.ok()).toBeTruthy();
  return (await created.json()).data as { id: string };
}

async function makeComment(
  request: APIRequestContext,
  headers: { Authorization: string },
  checkinID: string,
  body: string,
) {
  const created = await request.post(`/api/v1/checkins/${checkinID}/comments`, { headers, data: { body } });
  expect(created.ok()).toBeTruthy();
  return (await created.json()).data as { id: string };
}

test.describe('post interactions', () => {
  test('save and unsave a check-in while /saved stays private', async ({ page, browser, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);
    const headers = { Authorization: `Bearer ${token}` };

    const stamp = Date.now();
    const myNote = `alice saved note ${stamp}`;
    const mine = await makeCheckin(request, headers, myNote);

    const bob = await secondMember(browser);
    const bobNote = `bob private note ${stamp}`;
    const bobs = await makeCheckin(request, bob.headers, bobNote);
    await bob.context.close();

    await page.goto(`/checkins/${mine.id}`);
    const save = page.locator('.save-toggle');
    await expect(save).toHaveAttribute('aria-pressed', 'false');
    await save.click();
    await expect(page.locator('.save-toggle')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.save-toggle')).toContainText('Saved');
    await expectNoSeriousViolations(page);

    await page.goto('/saved');
    await expect(page.getByRole('heading', { name: 'Saved posts' })).toBeVisible();
    const savedItem = page.locator(`.checkin[data-checkin-id="${mine.id}"]`);
    await expect(savedItem).toBeVisible();
    await expect(savedItem).toContainText(myNote);
    await expect(page.locator(`.checkin[data-checkin-id="${bobs.id}"]`)).toHaveCount(0);
    await expect(page.getByText(bobNote)).toHaveCount(0);
    await expectNoSeriousViolations(page);

    const savedChip = savedItem.locator('.save-toggle');
    await expect(savedChip).toHaveAttribute('aria-pressed', 'true');
    await savedChip.click();
    await expect(page.getByText('No saved posts yet.')).toBeVisible();
    await expect(page.locator(`.checkin[data-checkin-id="${mine.id}"]`)).toHaveCount(0);
  });

  test('reply prefills the comment box with a mention and focuses it', async ({ page, browser, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);
    const headers = { Authorization: `Bearer ${token}` };
    const checkin = await makeCheckin(request, headers, `reply target ${Date.now()}`);

    const bob = await secondMember(browser);
    const body = 'Reply to me please.';
    await makeComment(request, bob.headers, checkin.id, body);
    await bob.context.close();

    await page.goto(`/checkins/${checkin.id}`);
    const comment = page.locator('.comment').filter({ hasText: body });
    await expect(comment).toBeVisible();
    await comment.getByRole('button', { name: 'Reply' }).click();

    const box = page.locator('#comment-body');
    await expect(box).toBeFocused();
    await expect(box).toHaveValue(`@${bob.user.handle} `);
  });

  test('report a comment and land back on the check-in', async ({ page, browser, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);
    const headers = { Authorization: `Bearer ${token}` };
    const checkin = await makeCheckin(request, headers, `report target ${Date.now()}`);

    const bob = await secondMember(browser);
    const body = 'This comment needs a report.';
    await makeComment(request, bob.headers, checkin.id, body);
    await bob.context.close();

    await page.goto(`/checkins/${checkin.id}`);
    const comment = page.locator('.comment').filter({ hasText: body });
    await comment.locator('details.comment-report summary').click();
    await comment.getByLabel('Reason').selectOption('off');

    const reported = page.waitForResponse(
      (res) => res.request().method() === 'POST' && new URL(res.url()).pathname === '/reports',
    );
    await comment.getByRole('button', { name: 'Send report' }).click();
    const response = await reported;
    expect(response.status()).toBe(302);
    await expect(page).toHaveURL(new RegExp(`/checkins/${checkin.id}$`));
    await expect(comment).toBeVisible();
  });
});
