import { expect, test } from '@playwright/test';
import { createToken, registerAndVerify } from './helpers';

const auth = (token: string) => ({ Authorization: `Bearer ${token}` });

test.describe('v1.1 API', () => {
  test('a verified member reads their notifications, check-in detail and insights', async ({ page, request }) => {
    await registerAndVerify(page);
    const token = await createToken(page);

    const search = await request.get('/api/v1/search?q=cola', { headers: auth(token) });
    expect(search.status()).toBe(200);
    const found = (await search.json()).data as Array<{ id: number; slug: string }>;
    expect(found.length).toBeGreaterThan(0);
    const drink = found[0];

    const created = await request.post('/api/v1/checkins', {
      headers: auth(token),
      data: { drink_id: drink.id, rating: 4.25, serving: 'can', visibility: 'public', note: 'e2e insight pour' },
    });
    expect([200, 201]).toContain(created.status());
    const checkin = (await created.json()).data as { id: string };

    const detail = await request.get(`/api/v1/checkins/${checkin.id}`, { headers: auth(token) });
    expect(detail.status()).toBe(200);
    const detailData = (await detail.json()).data as { drink: { slug: string }; user: { handle: string } };
    expect(detailData.drink.slug).toBe(drink.slug);
    expect(detailData.user.handle).toMatch(/^e2e_/);

    const notifications = await request.get('/api/v1/notifications', { headers: auth(token) });
    expect(notifications.status()).toBe(200);
    const inbox = (await notifications.json()) as { data: unknown[]; meta?: { unread?: number } };
    expect(Array.isArray(inbox.data)).toBe(true);

    const insights = await request.get('/api/v1/stats/me/insights?range=7', { headers: auth(token) });
    expect(insights.status()).toBe(200);
    const data = (await insights.json()).data as {
      range_days: number;
      pours: number;
      sugar_g: number;
      series: unknown[];
    };
    expect(data.range_days).toBe(7);
    expect(data.series).toHaveLength(7);
    expect(data.pours).toBe(1);
    expect(data.sugar_g).toBeGreaterThan(0);
  });

  test('another member gets 404 for a check-in that is not theirs', async ({ page, request, browser }) => {
    await registerAndVerify(page);
    const token = await createToken(page);

    const search = await request.get('/api/v1/search?q=cola', { headers: auth(token) });
    const drink = (await search.json()).data[0] as { id: number };

    const created = await request.post('/api/v1/checkins', {
      headers: auth(token),
      data: { drink_id: drink.id, serving: 'can', visibility: 'private' },
    });
    const checkin = (await created.json()).data as { id: string };

    const otherContext = await browser.newContext();
    const otherPage = await otherContext.newPage();
    await registerAndVerify(otherPage);
    const otherToken = await createToken(otherPage);
    await otherContext.close();

    const denied = await request.get(`/api/v1/checkins/${checkin.id}`, { headers: auth(otherToken) });
    expect(denied.status()).toBe(404);
    expect((await denied.json()).error.code).toBe('not_found');
  });

  test('the API rejects anonymous calls and the webhook hides whether a token exists', async ({ request }) => {
    const anonymous = await request.get('/api/v1/notifications');
    expect(anonymous.status()).toBe(401);
    expect(anonymous.headers()['www-authenticate']).toContain('Bearer');

    const hook = await request.post('/hooks/ha/not-a-real-token', {
      data: { slug: 'coca-cola-355' },
    });
    expect(hook.status()).toBe(401);
    expect(await hook.json()).toEqual({ error: 'unauthorized' });
    expect(hook.headers()['set-cookie']).toBeUndefined();
    expect(await hook.text()).not.toContain('not-a-real-token');
  });

  test('crawler headers keep member pages out of search results', async ({ request }) => {
    const home = await request.get('/');
    expect(home.status()).toBe(200);
    expect(home.headers()['x-robots-tag']).toBeUndefined();

    const browse = await request.get('/drinks', { maxRedirects: 0 });
    expect(browse.status()).toBe(302);
    expect(browse.headers()['location']).toContain('/login');
    expect(browse.headers()['x-robots-tag']).toContain('noindex');
  });
});
