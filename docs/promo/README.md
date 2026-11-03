# Promo assets

| File | What it is |
| --- | --- |
| `sodapop-promo.svg` | Source artwork (1600×1600). Edit this. |
| `sodapop-promo.png` | 1600×1600 render for social/README use. |
| `_render.html` | Rasterisation wrapper, not for publishing. |

The poster uses the app palette: cream `#F2F4E6`, lime `#B4E32C`, orange `#E85D26`, ink `#16241A`, and the wordmark's lime dot. Copy is "Scan. Rate. Repeat." plus the one-line descriptor.

## Regenerate the PNG

Requires the Playwright Chromium used by the E2E suite.

```sh
CHROME=$(ls -d ~/.cache/ms-playwright/chromium-*/chrome-linux64/chrome | tail -1)
"$CHROME" --headless=new --disable-gpu --no-sandbox --hide-scrollbars \
  --window-size=1600,1600 \
  --screenshot="$PWD/docs/promo/sodapop-promo.png" \
  "file://$PWD/docs/promo/_render.html"
```

Add `--force-device-scale-factor=2` alongside `--window-size` for a 3200×3200 export.
