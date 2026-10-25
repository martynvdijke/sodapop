import { defineConfig, devices } from '@playwright/test';

const db = '.e2e/sodapop.db';
const port = 8097;

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list']],
  timeout: 45_000,
  expect: { timeout: 8_000 },
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    reducedMotion: 'reduce',
    ...devices['Desktop Chrome'],
  },
  webServer: [
    {
      command: [
        'mkdir -p .e2e',
        `rm -f ${db} ${db}-wal ${db}-shm`,
        `DATABASE_PATH=${db} SEED_DEMO=1 SESSION_SECRET=e2e-secret-e2e-secret-e2e-secret go run ./cmd/seed`,
        `DATABASE_PATH=${db} SESSION_SECRET=e2e-secret-e2e-secret-e2e-secret PORT=${port} BASE_URL=http://127.0.0.1:${port} ALLOW_PASSWORD_FALLBACK=true ENV=development go run ./cmd/server`,
      ].join(' && '),
      url: `http://127.0.0.1:${port}/healthz`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
});
