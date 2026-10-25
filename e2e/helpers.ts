import { expect, type Page } from '@playwright/test';

export interface TestUser {
  handle: string;
  email: string;
  password: string;
}

export function newUser(): TestUser {
  const suffix = `${Date.now().toString(36)}${Math.floor(Math.random() * 1e4)}`;
  return {
    handle: `e2e_${suffix}`.slice(0, 24),
    email: `e2e_${suffix}@example.com`,
    password: 'correct-horse-battery',
  };
}

/** Registers a local account and completes the dev-only email verification. */
export async function registerAndVerify(page: Page, user = newUser()): Promise<TestUser> {
  await page.goto('/register');
  await page.getByLabel('Handle').fill(user.handle);
  await page.getByLabel('Email').fill(user.email);
  await page.getByLabel('Password', { exact: true }).fill(user.password);
  await page.getByRole('button', { name: 'Create account' }).click();

  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await page.getByRole('link', { name: /open the verification link/i }).click();
  await expect(page.getByText('Your email is confirmed')).toBeVisible();
  return user;
}

export async function signIn(page: Page, user: TestUser) {
  await page.goto('/login');
  await page.getByLabel('Email or handle').fill(user.handle);
  await page.getByLabel('Password', { exact: true }).fill(user.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/$/);
}

/** Creates a personal API token from the settings page and returns the raw sd_ value. */
export async function createToken(page: Page): Promise<string> {
  await page.goto('/settings/tokens');
  await page.getByLabel('Token name').fill('e2e');
  await page.getByRole('button', { name: /create token/i }).click();
  const token = page.locator('code.token-value');
  await expect(token).toBeVisible();
  return (await token.textContent())?.trim() ?? '';
}
