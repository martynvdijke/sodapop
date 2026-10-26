import { expect, test } from '@playwright/test';
import { registerAndVerify } from './helpers';

const COCA_COLA_GTIN = '5449000000996';

/**
 * Replaces the camera and the native barcode detector with deterministic
 * browser-level fakes. This exercises the real scanner state machine, frame
 * loop, redirect, and server lookup without requiring a physical camera.
 */
async function installFakeCamera(page: import('@playwright/test').Page) {
  await page.addInitScript((gtin) => {
    const canvas = document.createElement('canvas');
    canvas.width = 320;
    canvas.height = 240;
    const context = canvas.getContext('2d');
    if (!context) throw new Error('canvas 2d context unavailable');
    let frame = 0;
    setInterval(() => {
      context.fillStyle = frame++ % 2 === 0 ? '#ffffff' : '#f0f0f0';
      context.fillRect(0, 0, canvas.width, canvas.height);
    }, 100);

    const stream = canvas.captureStream(10);
    Object.defineProperty(navigator.mediaDevices, 'getUserMedia', {
      configurable: true,
      value: async () => stream,
    });

    class FakeBarcodeDetector {
      static async getSupportedFormats() {
        return ['ean_13', 'ean_8', 'upc_a', 'upc_e'];
      }

      async detect() {
        return [{ rawValue: gtin }];
      }
    }

    Object.defineProperty(window, 'BarcodeDetector', {
      configurable: true,
      value: FakeBarcodeDetector,
    });
  }, COCA_COLA_GTIN);
}

test.describe('barcode scanner', () => {
  test('camera scan resolves a barcode and opens its check-in', async ({ page }) => {
    await registerAndVerify(page);
    await installFakeCamera(page);

    await page.goto('/scan');
    await expect(page.getByRole('heading', { name: 'Scan or enter a barcode' })).toBeVisible();

    await page.getByRole('button', { name: 'Scan barcode' }).click();

    await expect(page).toHaveURL(new RegExp(`/scan\\?gtin=${COCA_COLA_GTIN}$`));
    await expect(page.getByRole('heading', { name: 'Coca-Cola', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Save check-in' })).toBeVisible();
  });

  test('denied camera keeps manual entry usable', async ({ page }) => {
    await registerAndVerify(page);
    await page.addInitScript(() => {
      Object.defineProperty(navigator.mediaDevices, 'getUserMedia', {
        configurable: true,
        value: async () => {
          throw new DOMException('permission denied', 'NotAllowedError');
        },
      });
    });

    await page.goto('/scan');
    await page.getByRole('button', { name: 'Scan barcode' }).click();

    await expect(page.getByText('Camera access was blocked. Type the barcode instead.')).toBeVisible();

    const scannerForm = page.locator('.scanner-manual');
    await scannerForm.getByLabel('Barcode digits').fill(COCA_COLA_GTIN);
    await scannerForm.getByRole('button', { name: 'Look up barcode' }).click();

    await expect(page).toHaveURL(new RegExp(`/scan\\?gtin=${COCA_COLA_GTIN}$`));
    await expect(page.getByRole('heading', { name: 'Coca-Cola', exact: true })).toBeVisible();
  });

  test('manual entry never requests the camera', async ({ page }) => {
    await registerAndVerify(page);
    await page.addInitScript(() => {
      Object.defineProperty(navigator.mediaDevices, 'getUserMedia', {
        configurable: true,
        value: async () => {
          (window as Window & { __cameraCalls?: number }).__cameraCalls =
            ((window as Window & { __cameraCalls?: number }).__cameraCalls ?? 0) + 1;
          throw new DOMException('camera should not be requested', 'NotAllowedError');
        },
      });
    });

    await page.goto('/scan');
    const form = page.locator('.scanner-manual');
    await expect(form).toBeVisible();
    expect(await page.evaluate(() => (window as Window & { __cameraCalls?: number }).__cameraCalls ?? 0)).toBe(0);

    await form.getByLabel('Barcode digits').fill(COCA_COLA_GTIN);
    await form.getByRole('button', { name: 'Look up barcode' }).click();

    await expect(page).toHaveURL(new RegExp(`/scan\\?gtin=${COCA_COLA_GTIN}$`));
    await expect(page.getByRole('heading', { name: 'Coca-Cola', exact: true })).toBeVisible();
    expect(await page.evaluate(() => (window as Window & { __cameraCalls?: number }).__cameraCalls ?? 0)).toBe(0);
  });
});
