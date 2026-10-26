import { expect, test, type APIRequestContext, type Browser } from '@playwright/test';
import { createToken, newUser, registerAndVerify, type TestUser } from './helpers';

const auth = (token: string) => ({ Authorization: `Bearer ${token}` });

interface Drink {
  id: number;
  slug: string;
  name: string;
}

interface CheckinDetail {
  id: string;
  user: { handle: string; display_name: string };
  toasts: number;
  toasted: boolean;
  comments_count: number;
  can_delete: boolean;
  can_toast: boolean;
  can_comment: boolean;
}

async function makeVerifiedUser(browser: Browser): Promise<{ user: TestUser; token: string }> {
  const context = await browser.newContext();
  const page = await context.newPage();
  const user = newUser();
  await registerAndVerify(page, user);
  const token = await createToken(page);
  await context.close();
  expect(token).toMatch(/^sd_/);
  return { user, token };
}

function expectNoTotals(body: Record<string, unknown>) {
  expect(body).not.toHaveProperty('total');
  if (body.meta && typeof body.meta === 'object') {
    expect(body.meta).not.toHaveProperty('total');
  }
}

async function searchDrinks(request: APIRequestContext, token: string, q: string): Promise<Drink[]> {
  const res = await request.get(`/api/v1/search?q=${encodeURIComponent(q)}`, { headers: auth(token) });
  expect(res.status()).toBe(200);
  const body = await res.json();
  expect(Array.isArray(body.data)).toBe(true);
  return body.data as Drink[];
}

async function createCheckin(
  request: APIRequestContext,
  token: string,
  drinkID: number,
  overrides: Record<string, unknown> = {},
): Promise<{ id: string }> {
  const res = await request.post('/api/v1/checkins', {
    headers: auth(token),
    data: { drink_id: drinkID, rating: 4, serving: 'can', visibility: 'public', ...overrides },
  });
  expect([200, 201]).toContain(res.status());
  return (await res.json()).data as { id: string };
}

async function getCheckin(request: APIRequestContext, token: string, id: string): Promise<CheckinDetail> {
  const res = await request.get(`/api/v1/checkins/${id}`, { headers: auth(token) });
  expect(res.status()).toBe(200);
  return (await res.json()).data as CheckinDetail;
}

test.describe.serial('v1.2 API parity', () => {
  let a!: TestUser;
  let b!: TestUser;
  let tokenA = '';
  let tokenB = '';

  test.beforeAll(async ({ browser }) => {
    ({ user: a, token: tokenA } = await makeVerifiedUser(browser));
    ({ user: b, token: tokenB } = await makeVerifiedUser(browser));
  });

  test('check-in detail flags, comments, and cross-user visibility', async ({ request }) => {
    const drinks = await searchDrinks(request, tokenA, 'cola');
    expect(drinks.length).toBeGreaterThan(0);

    const checkin = await createCheckin(request, tokenA, drinks[0].id, { note: 'parity comment target' });

    const own = await getCheckin(request, tokenA, checkin.id);
    expect(own.can_delete).toBe(true);
    expect(own.can_toast).toBe(false);
    expect(own.can_comment).toBe(true);
    expect(own.comments_count).toBe(0);
    expect(own.user.handle).toBe(a.handle);

    // A different signed-in member can see the public row but cannot delete it.
    const asB = await getCheckin(request, tokenB, checkin.id);
    expect(asB.can_delete).toBe(false);
    expect(asB.can_toast).toBe(true);
    expect(asB.can_comment).toBe(true);

    const commentBody = `parity comment ${Date.now()}`;
    const commentRes = await request.post(`/api/v1/checkins/${checkin.id}/comments`, {
      headers: auth(tokenB),
      data: { body: commentBody },
    });
    expect(commentRes.status()).toBe(201);
    const comment = (await commentRes.json()).data as { id: string; can_delete: boolean };

    const afterComment = await getCheckin(request, tokenA, checkin.id);
    expect(afterComment.comments_count).toBe(1);

    const listRes = await request.get(`/api/v1/checkins/${checkin.id}/comments`, { headers: auth(tokenB) });
    expect(listRes.status()).toBe(200);
    const listed = (await listRes.json()).data as Array<{ id: string; body: string; can_delete: boolean }>;
    const mine = listed.find((c) => c.id === comment.id);
    expect(mine).toBeTruthy();
    expect(mine?.can_delete).toBe(true);

    const deleted = await request.delete(`/api/v1/comments/${comment.id}`, { headers: auth(tokenB) });
    expect(deleted.status()).toBe(204);
    expect((await getCheckin(request, tokenA, checkin.id)).comments_count).toBe(0);

    // A private row stays invisible (404) to another member.
    const privateCheckin = await createCheckin(request, tokenA, drinks[0].id, { visibility: 'private' });
    const hidden = await request.get(`/api/v1/checkins/${privateCheckin.id}`, { headers: auth(tokenB) });
    expect(hidden.status()).toBe(404);
    expect((await hidden.json()).error.code).toBe('not_found');
  });

  test('profile stats, arrays, and follow round-trip', async ({ request }) => {
    const profileRes = await request.get(`/api/v1/users/${a.handle}`, { headers: auth(tokenA) });
    expect(profileRes.status()).toBe(200);
    const body = await profileRes.json();
    expectNoTotals(body);
    const profile = body.data as Record<string, any>;

    expect(profile.handle).toBe(a.handle);
    expect(profile.stats).toMatchObject({
      checkins: expect.any(Number),
      distinct_drinks: expect.any(Number),
      toasts_received: expect.any(Number),
    });
    expect(typeof profile.follower_count).toBe('number');
    expect(typeof profile.following_count).toBe('number');
    expect(profile.is_following).toBe(false);
    expect(Array.isArray(profile.badges)).toBe(true);
    expect(Array.isArray(profile.top_styles)).toBe(true);
    expect(Array.isArray(profile.lists)).toBe(true);
    for (const badge of profile.badges) {
      expect(typeof badge.slug).toBe('string');
      expect(typeof badge.name).toBe('string');
    }
    for (const style of profile.top_styles) {
      expect(typeof style.slug).toBe('string');
      expect(typeof style.count).toBe('number');
    }
    const followersBefore = profile.follower_count as number;

    const followed = await request.post(`/api/v1/users/${a.handle}/follow`, { headers: auth(tokenB) });
    expect(followed.status()).toBe(204);

    const fromBRes = await request.get(`/api/v1/users/${a.handle}`, { headers: auth(tokenB) });
    expect(fromBRes.status()).toBe(200);
    const fromB = (await fromBRes.json()).data as Record<string, any>;
    expect(fromB.is_following).toBe(true);
    expect(fromB.follower_count).toBe(followersBefore + 1);

    // Following is idempotent.
    expect((await request.post(`/api/v1/users/${a.handle}/follow`, { headers: auth(tokenB) })).status()).toBe(204);

    expect((await request.delete(`/api/v1/users/${a.handle}/follow`, { headers: auth(tokenB) })).status()).toBe(204);
    const afterUnfollow = (await (await request.get(`/api/v1/users/${a.handle}`, { headers: auth(tokenB) })).json()).data;
    expect(afterUnfollow.is_following).toBe(false);
    expect(afterUnfollow.follower_count).toBe(followersBefore);
  });

  test('block state is indistinguishable from an unknown handle', async ({ request }) => {
    const unknown = `e2e_gone_${Date.now().toString(36)}`.slice(0, 24);
    const unknownRes = await request.post(`/api/v1/users/${unknown}/follow`, { headers: auth(tokenA) });
    expect(unknownRes.status()).toBe(404);
    const unknownBody = await unknownRes.json();
    expect(unknownBody.error.code).toBe('not_found');

    const blockRes = await request.post(`/api/v1/users/${a.handle}/block`, {
      headers: auth(tokenB),
      data: {},
    });
    expect(blockRes.status()).toBe(200);
    expect((await blockRes.json()).data.blocked).toBe(true);

    const blockedRes = await request.post(`/api/v1/users/${b.handle}/follow`, { headers: auth(tokenA) });
    expect(blockedRes.status()).toBe(404);
    expect(blockedRes.status()).not.toBe(403);
    expect(await blockedRes.json()).toEqual(unknownBody);

    // The blocked profile is also invisible to A.
    expect((await request.get(`/api/v1/users/${b.handle}`, { headers: auth(tokenA) })).status()).toBe(404);

    const unblockRes = await request.delete(`/api/v1/users/${a.handle}/block`, { headers: auth(tokenB) });
    expect(unblockRes.status()).toBe(200);
    expect((await unblockRes.json()).data.blocked).toBe(false);
    expect((await request.get(`/api/v1/users/${b.handle}`, { headers: auth(tokenA) })).status()).toBe(200);
  });

  test('notifications carry safe hrefs, actor initials, and friend request ids', async ({ request }) => {
    const drinks = await searchDrinks(request, tokenA, 'cola');
    const checkin = await createCheckin(request, tokenA, drinks[0].id, { note: 'parity notification target' });

    const commentRes = await request.post(`/api/v1/checkins/${checkin.id}/comments`, {
      headers: auth(tokenB),
      data: { body: `notify me ${Date.now()}` },
    });
    expect(commentRes.status()).toBe(201);
    const comment = (await commentRes.json()).data as { id: string };

    const requestRes = await request.post('/api/v1/friends/requests', {
      headers: auth(tokenB),
      data: { handle: a.handle },
    });
    expect(requestRes.status()).toBe(202);

    const inboxRes = await request.get('/api/v1/notifications', { headers: auth(tokenA) });
    expect(inboxRes.status()).toBe(200);
    const inboxBody = await inboxRes.json();
    expectNoTotals(inboxBody);
    expect(Array.isArray(inboxBody.data)).toBe(true);
    const inbox = inboxBody.data as Array<Record<string, any>>;
    expect(inboxBody.meta.unread).toBeGreaterThan(0);

    const commentItem = inbox.find((n) => n.kind === 'comment' && n.actor?.handle === b.handle);
    expect(commentItem).toBeTruthy();
    expect(commentItem?.subject_id).toBe(checkin.id);
    expect(commentItem?.href?.startsWith('/')).toBe(true);
    expect(typeof commentItem?.actor_initial).toBe('string');
    expect((commentItem?.actor_initial as string).length).toBeGreaterThan(0);

    const friendItem = inbox.find((n) => n.kind === 'friend_request' && n.actor?.handle === b.handle);
    expect(friendItem).toBeTruthy();
    expect(friendItem?.href?.startsWith('/')).toBe(true);
    expect(typeof friendItem?.friend_request_id).toBe('string');
    expect((friendItem?.friend_request_id as string).length).toBeGreaterThan(0);

    const readRes = await request.post('/api/v1/notifications/read', { headers: auth(tokenA) });
    // The v1.2 surface documents 200 with the number marked; accept 204 too so the
    // spec does not hard-code an implementation detail.
    expect([200, 204]).toContain(readRes.status());

    const afterRes = await request.get('/api/v1/notifications', { headers: auth(tokenA) });
    const after = await afterRes.json();
    const afterItems = after.data as Array<Record<string, any>>;
    const unread = after.meta.unread as number;
    expect(unread === 0 || afterItems.every((n) => n.read === true)).toBe(true);

    // Clean up the pending friendship and the comment that produced the events.
    const friendshipID = friendItem?.friend_request_id as string;
    const removed = await request.delete(`/api/v1/friends/${friendshipID}`, { headers: auth(tokenA) });
    expect(removed.status()).toBe(204);
    expect((await request.delete(`/api/v1/comments/${comment.id}`, { headers: auth(tokenB) })).status()).toBe(204);
  });

  test('saved venues round-trip through the personal collection', async ({ request }) => {
    const name = `E2E Fizz Stop ${Date.now()}`;
    const venueRes = await request.post('/api/v1/venues', {
      headers: auth(tokenA),
      data: { name, kind: 'shop', city: 'Testville', country: 'NL' },
    });
    expect([200, 201]).toContain(venueRes.status());
    const venue = (await venueRes.json()).data as { id: number; name: string };

    const searchRes = await request.get(`/api/v1/venues/search?q=${encodeURIComponent(name)}`, {
      headers: auth(tokenA),
    });
    expect(searchRes.status()).toBe(200);
    const searchBody = await searchRes.json();
    expectNoTotals(searchBody);
    expect(Array.isArray(searchBody.data)).toBe(true);
    const found = (searchBody.data as Array<{ id: number; name: string }>).find((v) => v.id === venue.id);
    expect(found).toBeTruthy();

    const savedRes = await request.post(`/api/v1/venues/${venue.id}/save`, { headers: auth(tokenA), data: {} });
    expect(savedRes.status()).toBe(200);
    expect((await savedRes.json()).data.saved).toBe(true);

    const listSaved = await request.get('/api/v1/venues/saved', { headers: auth(tokenA) });
    expect(listSaved.status()).toBe(200);
    const savedBody = await listSaved.json();
    expectNoTotals(savedBody);
    const savedIDs = (savedBody.data as Array<{ id: number }>).map((v) => v.id);
    expect(savedIDs).toContain(venue.id);

    const unsavedRes = await request.delete(`/api/v1/venues/${venue.id}/save`, { headers: auth(tokenA) });
    expect(unsavedRes.status()).toBe(200);
    expect((await unsavedRes.json()).data.saved).toBe(false);

    const afterRes = await request.get('/api/v1/venues/saved', { headers: auth(tokenA) });
    const afterIDs = ((await afterRes.json()).data as Array<{ id: number }>).map((v) => v.id);
    expect(afterIDs).not.toContain(venue.id);
  });

  test('personal lists include the system wishlist without totals', async ({ request }) => {
    const listsRes = await request.get('/api/v1/me/lists', { headers: auth(tokenA) });
    expect(listsRes.status()).toBe(200);
    const listsBody = await listsRes.json();
    expectNoTotals(listsBody);
    expect(Array.isArray(listsBody.data)).toBe(true);
    const lists = listsBody.data as Array<Record<string, any>>;
    const wishlist = lists.find((l) => l.is_system === true);
    expect(wishlist).toBeTruthy();
    expect(typeof wishlist?.id).toBe('string');
    expect(typeof wishlist?.item_count).toBe('number');

    const listRes = await request.get(`/api/v1/lists/${wishlist?.id}`, { headers: auth(tokenA) });
    expect(listRes.status()).toBe(200);
    const listBody = await listRes.json();
    expectNoTotals(listBody);
    expectNoTotals(listBody.data);
    expect(Array.isArray(listBody.data.items)).toBe(true);

    // The system wishlist is owner-only.
    const otherRes = await request.get(`/api/v1/lists/${wishlist?.id}`, { headers: auth(tokenB) });
    expect(otherRes.status()).toBe(404);
  });

  test('mix authoring validates component counts and reads back components', async ({ request }) => {
    const seen = new Map<number, Drink>();
    for (const q of ['cola', 'ginger']) {
      for (const drink of await searchDrinks(request, tokenA, q)) {
        seen.set(drink.id, drink);
      }
    }
    const drinks = [...seen.values()];
    expect(drinks.length).toBeGreaterThanOrEqual(2);

    const listRes = await request.get('/api/v1/mix', { headers: auth(tokenA) });
    expect(listRes.status()).toBe(200);
    const listBody = await listRes.json();
    expectNoTotals(listBody);
    expect(Array.isArray(listBody.data)).toBe(true);

    const components = [
      { drink_id: drinks[0].id, volume_ml: 150 },
      { drink_id: drinks[1].id, volume_ml: 180 },
    ];
    const createRes = await request.post('/api/v1/mix', {
      headers: auth(tokenA),
      data: { name: `E2E Mix ${Date.now()}`, visibility: 'public', components },
    });
    expect([200, 201]).toContain(createRes.status());
    const mix = (await createRes.json()).data as { id: number; components: Array<Record<string, any>> };
    expect(typeof mix.id).toBe('number');
    expect(mix.components).toHaveLength(2);

    const getRes = await request.get(`/api/v1/mix/${mix.id}`, { headers: auth(tokenA) });
    expect(getRes.status()).toBe(200);
    const readBack = (await getRes.json()).data as { components: Array<Record<string, any>> };
    expect(readBack.components).toHaveLength(2);
    expect(readBack.components.map((c) => c.drink_id).sort()).toEqual([drinks[0].id, drinks[1].id].sort());
    for (const component of readBack.components) {
      expect(typeof component.volume_ml).toBe('number');
      expect(typeof component.name).toBe('string');
    }

    const tooFew = await request.post('/api/v1/mix', {
      headers: auth(tokenA),
      data: { name: 'E2E One Part', components: [{ drink_id: drinks[0].id, volume_ml: 150 }] },
    });
    expect(tooFew.status()).toBe(400);

    // Seven entries: the component-count guard rejects the request before the
    // distinct-drink check, so recycling the small demo catalog is enough here.
    const tooMany = await request.post('/api/v1/mix', {
      headers: auth(tokenA),
      data: {
        name: 'E2E Seven Parts',
        components: Array.from({ length: 7 }, (_, i) => ({
          drink_id: drinks[i % drinks.length].id,
          volume_ml: 100 + i * 10,
        })),
      },
    });
    expect(tooMany.status()).toBe(400);
  });

  test('taste endpoints return personal arrays', async ({ request }) => {
    for (const path of ['/api/v1/taste/twins', '/api/v1/taste/recommendations']) {
      const res = await request.get(path, { headers: auth(tokenA) });
      expect(res.status()).toBe(200);
      const body = await res.json();
      expectNoTotals(body);
      expect(Array.isArray(body.data)).toBe(true);
    }
  });

  test('badges and passport are personal and carry no totals', async ({ request }) => {
    const badgesRes = await request.get('/api/v1/badges', { headers: auth(tokenA) });
    expect(badgesRes.status()).toBe(200);
    const badgesBody = await badgesRes.json();
    expectNoTotals(badgesBody);
    expect(Array.isArray(badgesBody.data)).toBe(true);
    for (const badge of badgesBody.data as Array<Record<string, any>>) {
      expect(typeof badge.slug).toBe('string');
      expect(typeof badge.earned).toBe('boolean');
      expect(typeof badge.percent).toBe('number');
    }

    const passportRes = await request.get('/api/v1/passport', { headers: auth(tokenA) });
    expect(passportRes.status()).toBe(200);
    const passportBody = await passportRes.json();
    expectNoTotals(passportBody);
    expectNoTotals(passportBody.data);
    expect(passportBody.data.stats).toBeTruthy();
    expect(Array.isArray(passportBody.data.badges)).toBe(true);
  });

  // Kept last so the known `can_toast` contract gap does not stop the rest of
  // the serial coverage from running. See the lane report.
  test('toast toggling updates the count and the viewer flag', async ({ request }) => {
    const drinks = await searchDrinks(request, tokenA, 'cola');
    const checkin = await createCheckin(request, tokenA, drinks[0].id, { note: 'parity toast target' });

    const before = await getCheckin(request, tokenB, checkin.id);
    expect(before.toasts).toBe(0);
    expect(before.toasted).toBe(false);
    expect(before.can_toast).toBe(true);

    const toasted = await request.post(`/api/v1/checkins/${checkin.id}/toast`, { headers: auth(tokenB) });
    expect(toasted.status()).toBe(200);
    expect((await toasted.json()).data.toasted).toBe(true);

    const ownerView = await getCheckin(request, tokenA, checkin.id);
    expect(ownerView.toasts).toBe(1);
    expect(ownerView.toasted).toBe(false);
    expect(ownerView.can_toast).toBe(false);

    const toasterView = await getCheckin(request, tokenB, checkin.id);
    expect(toasterView.toasts).toBe(1);
    expect(toasterView.toasted).toBe(true);
    // Already toasted: the action must be unavailable.
    expect(toasterView.can_toast).toBe(false);

    const untoasted = await request.delete(`/api/v1/checkins/${checkin.id}/toast`, { headers: auth(tokenB) });
    expect(untoasted.status()).toBe(200);
    expect((await untoasted.json()).data.toasted).toBe(false);
    expect((await getCheckin(request, tokenA, checkin.id)).toasts).toBe(0);
  });
});
