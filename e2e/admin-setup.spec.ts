import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { signIn } from './helpers';

const setupBase = 'http://127.0.0.1:8098';
const setupToken = 'e2e-setup-token';
const admin = { handle: 'e2e_admin', email: 'e2e_admin@example.com', password: 'correct-horse-battery' };

let setupServer: ChildProcess | undefined;
let serverLog = '';

test.describe.configure({ mode: 'serial' });

async function expectNoSeriousViolations(page: Page, path: string) {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(serious.map((v) => `${v.id}: ${v.help}`), `serious violations on ${path}`).toEqual([]);
}

async function assertPortFree(port: number) {
  await new Promise<void>((resolve, reject) => {
    const probe = createServer();
    probe.once('error', (err: NodeJS.ErrnoException) => {
      if (err.code === 'EADDRINUSE') {
        reject(new Error(`port ${port} is already in use; stop the process using it before running e2e/admin-setup.spec.ts`));
        return;
      }
      reject(err);
    });
    probe.once('listening', () => probe.close(() => resolve()));
    probe.listen(port, '127.0.0.1');
  });
}

test.beforeAll(async () => {
  await assertPortFree(8098);
  for (const suffix of ['', '-wal', '-shm']) {
    rmSync(`.e2e/setup.db${suffix}`, { force: true });
  }
  rmSync('.e2e/sodapop-setup', { force: true });
  execFileSync('go', ['build', '-o', '.e2e/sodapop-setup', './cmd/server'], { stdio: 'inherit' });

  setupServer = spawn('.e2e/sodapop-setup', [], {
    env: {
      ...process.env,
      ENV: 'development',
      PORT: '8098',
      DATABASE_PATH: '.e2e/setup.db',
      BASE_URL: setupBase,
      SESSION_SECRET: 'e2e-setup-secret-e2e-setup-secret',
      SETUP_TOKEN: setupToken,
      ALLOW_PASSWORD_FALLBACK: 'true',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  setupServer.stdout?.on('data', (chunk) => {
    serverLog += chunk.toString();
  });
  setupServer.stderr?.on('data', (chunk) => {
    serverLog += chunk.toString();
  });

  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${setupBase}/setup`);
      if (response.status === 200) return;
    } catch {
      // Server is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`setup server did not start:\n${serverLog}`);
});

test.afterAll(async () => {
  const child = setupServer;
  if (child && child.exitCode === null) {
    await new Promise<void>((resolve) => {
      const force = setTimeout(() => {
        child.kill('SIGKILL');
      }, 10_000);
      child.once('exit', () => {
        clearTimeout(force);
        resolve();
      });
      child.kill('SIGTERM');
    });
  }
  for (const suffix of ['', '-wal', '-shm']) {
    rmSync(`.e2e/setup.db${suffix}`, { force: true });
  }
  rmSync('.e2e/sodapop-setup', { force: true });
});

test('first-run setup creates the first admin and then locks itself', async ({ page }) => {
  await page.goto(`${setupBase}/setup`);
  await expect(page.getByRole('heading', { name: 'Set up Sodapop' })).toBeVisible();
  await expect(page.getByText('The token is never shown on this page.')).toBeVisible();
  await expectNoSeriousViolations(page, '/setup');

  await page.locator('#setup-token').fill(setupToken);
  await page.getByLabel('Handle').fill('setup_admin');
  await page.getByLabel('Email').fill('setup_admin@example.com');
  await page.locator('#setup-password').fill('correct-horse-battery');
  await page.getByRole('button', { name: 'Create admin account' }).click();

  await expect(page).toHaveURL(`${setupBase}/admin/settings`);
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Observability', level: 2 })).toBeVisible();

  const again = await page.goto(`${setupBase}/setup`);
  expect(again?.status()).toBe(404);

  const replay = await page.request.post(`${setupBase}/setup`, {
    form: {
      token: setupToken,
      handle: 'setup_admin_replay',
      email: 'setup_admin_replay@example.com',
      password: 'correct-horse-battery',
      display_name: 'Replay Admin',
    },
    failOnStatusCode: false,
  });
  expect(replay.status()).toBe(404);
});

test('admin can change runtime settings and close registration', async ({ page, browser }) => {
  await signIn(page, admin);
  await page.goto('/admin/settings');
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: /settings sections/i })).toBeVisible();

  const registration = page.getByLabel('Allow new registrations');
  const wasOpen = await registration.isChecked();
  try {
    if (wasOpen) {
      await registration.uncheck();
      await page.getByRole('button', { name: 'Save access' }).click();
      await expect(page.getByText('Access settings saved.')).toBeVisible();
    }

    const anonymous = await browser.newContext();
    const anonPage = await anonymous.newPage();
    const closed = await anonPage.goto('/register');
    expect(closed?.status()).toBe(200);
    await expect(anonPage.getByText('Registration is closed.')).toBeVisible();
    await expect(anonPage.getByRole('button', { name: 'Create account' })).toHaveCount(0);
    await anonymous.close();
  } finally {
    if (wasOpen) {
      await page.goto('/admin/settings');
      await page.getByLabel('Allow new registrations').check();
      await page.getByRole('button', { name: 'Save access' }).click();
      await expect(page.getByText('Access settings saved.')).toBeVisible();
    }
  }

  await page.goto('/admin/settings');
  await expectNoSeriousViolations(page, '/admin/settings');
});
