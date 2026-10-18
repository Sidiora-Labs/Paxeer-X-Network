import { test, expect, type Page } from "@playwright/test";

async function openTrade(page: Page, path = "/trade") {
  const response = await page.goto(path, { waitUntil: "domcontentloaded" });
  expect(response?.status()).toBe(200);
  await expect(
    page.getByText("Order book", { exact: true }).first(),
  ).toBeVisible();
}

test("SDK layout choices persist after reload", async ({ page }) => {
  await openTrade(page);
  await page.getByText("Layout", { exact: true }).click();
  const popup = page.locator("[data-radix-popper-content-wrapper]");
  await expect(popup.getByText("Advanced", { exact: true })).toBeVisible();
  await popup.getByText("Left", { exact: true }).first().click();
  await page.keyboard.press("Escape");
  const entry = page.locator(".oui-trading-orderEntry-container").first();
  await expect
    .poll(async () => (await entry.boundingBox())?.x)
    .toBeLessThan(500);
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect
    .poll(async () => (await entry.boundingBox())?.x)
    .toBeLessThan(500);
});

test("SDK split panels resize and preserve their width", async ({ page }) => {
  await openTrade(page);
  const entry = page.locator(".oui-trading-orderEntry-container").first();
  const before = (await entry.boundingBox())!;
  const bars = await page.locator(".w-split-bar").evaluateAll((nodes) =>
    nodes.map((node, index) => {
      const rect = node.getBoundingClientRect();
      return {
        index,
        x: rect.x,
        y: rect.y,
        width: rect.width,
        height: rect.height,
      };
    }),
  );
  const bar = bars
    .filter((item) => item.width < 5 && item.height > 100)
    .sort((a, b) => b.x - a.x)[0];
  expect(bar).toBeTruthy();
  await page.mouse.move(bar.x + bar.width / 2, bar.y + 100);
  await page.mouse.down();
  await page.mouse.move(bar.x - 80, bar.y + 100, { steps: 12 });
  await page.mouse.up();
  await expect
    .poll(async () => (await entry.boundingBox())?.width ?? 0)
    .toBeGreaterThan(before.width + 40);
  const after = (await entry.boundingBox())!;
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect
    .poll(async () =>
      Math.abs(((await entry.boundingBox())?.width ?? 0) - after.width),
    )
    .toBeLessThan(8);
});

test("SDK grid preset picker changes arrangement and saves the selection", async ({
  page,
}) => {
  await openTrade(page, "/trade/grid");
  const picker = page.getByRole("combobox", { name: "Grid layout preset" });
  await expect(picker).toBeVisible();
  await expect(picker.locator("option")).toHaveCount(2);
  const entry = page
    .locator(".react-grid-item")
    .filter({ has: page.getByText("Buy / Long", { exact: true }) })
    .first();
  await expect
    .poll(async () => (await entry.boundingBox())?.x ?? 0)
    .toBeGreaterThan(1000);
  const right = (await entry.boundingBox())!.x;
  await picker.selectOption("order-entry-left");
  await expect
    .poll(async () => (await entry.boundingBox())?.x ?? right)
    .toBeLessThan(right - 400);
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(picker).toHaveValue("order-entry-left");
  await expect
    .poll(async () => (await entry.boundingBox())?.x ?? right)
    .toBeLessThan(right - 400);
  await expect(page.locator(".react-grid-item").first()).toBeVisible();
  await expect(page.locator(".react-resizable-handle").first()).toBeAttached();
  const book = page
    .locator(".react-grid-item")
    .filter({ has: page.getByText("Order book", { exact: true }) })
    .first();
  const before = (await book.boundingBox())!;
  const handle = (await book
    .locator(".react-resizable-handle")
    .first()
    .boundingBox())!;
  await page.mouse.move(
    handle.x + handle.width / 2,
    handle.y + handle.height / 2,
  );
  await page.mouse.down();
  await page.mouse.move(handle.x + 160, handle.y + 28, { steps: 12 });
  await page.mouse.up();
  await expect
    .poll(async () => (await book.boundingBox())?.width ?? 0)
    .toBeGreaterThan(before.width + 70);
  const width = (await book.boundingBox())!.width;
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect
    .poll(async () =>
      Math.abs(((await book.boundingBox())?.width ?? 0) - width),
    )
    .toBeLessThan(8);
});

test("desktop routes render without runtime errors or exchange API traffic", async ({
  page,
}) => {
  const errors: string[] = [];
  const exchangeRequests: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (
      new RegExp(
        "^/v1/(?:client|order|public/(?:chain_info|token))(?:/|$)",
      ).test(new URL(request.url()).pathname)
    )
      exchangeRequests.push(request.url());
  });
  for (const route of [
    "/trade",
    "/portfolio",
    "/portfolio/positions",
    "/portfolio/orders",
    "/portfolio/assets",
    "/markets",
  ]) {
    const response = await page.goto(route, { waitUntil: "domcontentloaded" });
    expect(response?.status()).toBe(200);
    await expect(
      page.getByRole("button", { name: "Connect wallet", exact: true }).first(),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  }
  expect(errors).toEqual([]);
  expect(exchangeRequests).toEqual([]);
});

test("SDK mobile navigation and native wallet dialog work", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const response = await page.goto("/trade", { waitUntil: "domcontentloaded" });
  expect(response?.status()).toBe(200);
  await expect(page.getByText("Markets", { exact: true }).last()).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
  ).toBe(false);
  await page.getByText("Markets", { exact: true }).last().click();
  await expect(page).toHaveURL(/\/markets$/);
  await page.getByText("Portfolio", { exact: true }).last().click();
  await expect(page).toHaveURL(/\/portfolio$/);
  await page
    .getByRole("button", { name: "Connect", exact: true })
    .first()
    .click();
  await expect(page.getByRole("dialog")).toBeVisible();
  expect(errors).toEqual([]);
});

for (const restoredWallet of ["available", "locked", "removed"] as const) {
  test(`saved injected wallet reloads safely when ${restoredWallet}`, async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript(() => {
      const writes: string[] = [];
      (window as any).__nativeTestWrites = writes;
      const walletState = sessionStorage.getItem("native-test-wallet-state");
      if (walletState === "removed") return;
      const account = "0x000000000000000000000000000000000000bEEF";
      const provider = {
        isMetaMask: true,
        isConnected: () => true,
        on: () => {},
        removeListener: () => {},
        request: async ({ method }: { method: string }) => {
          if (/sign|sendTransaction|sendRawTransaction/i.test(method)) {
            writes.push(method);
            throw new Error(
              "This read-only browser fixture forbids signatures and writes.",
            );
          }
          if (method === "eth_accounts" || method === "eth_requestAccounts")
            return walletState === "locked" ? [] : [account];
          if (method === "eth_chainId") return "0x7d";
          if (method === "net_version") return "125";
          if (
            method === "wallet_switchEthereumChain" ||
            method === "wallet_requestPermissions"
          )
            return null;
          throw new Error(`Unsupported fixture request: ${method}`);
        },
      };
      Object.defineProperty(window, "ethereum", {
        value: provider,
        configurable: true,
      });
    });
    await openTrade(page);
    await page
      .getByRole("button", { name: "Connect wallet", exact: true })
      .first()
      .click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await dialog
      .getByRole("button", { name: /Injected|MetaMask/ })
      .first()
      .click();
    await expect(dialog).not.toBeVisible();
    await expect(
      page.getByRole("button", { name: "Connect wallet", exact: true }).first(),
    ).not.toBeVisible();
    expect(
      await page.evaluate(() => (window as any).__nativeTestWrites),
    ).toEqual([]);
    // Use the actual connection persisted by Wagmi, whose serialized connector
    // contains metadata only. Restoring it must wait for the live connector.
    const stored = await page.evaluate(() =>
      JSON.parse(localStorage.getItem("paxeer-wallet.store")!),
    );
    expect(stored.state.connections.value).toHaveLength(1);
    expect(stored.state.connections.value[0][1].connector).not.toHaveProperty(
      "getProvider",
    );
    await page.evaluate((state) => {
      sessionStorage.setItem("native-test-wallet-state", state);
    }, restoredWallet);
    await openTrade(page);
    const connectButton = page
      .getByRole("button", {
        name: "Connect wallet",
        exact: true,
      })
      .first();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    if (restoredWallet === "available") {
      await expect(connectButton).not.toBeVisible();
      await page.goto("/portfolio");
      const addressButton = page.getByTestId("oui-testid-nav-bar-address-btn");
      await expect(addressButton).toBeVisible();
      await expect(page.getByRole("dialog")).not.toBeVisible();
      await addressButton.click();
      await page
        .getByTestId("oui-testid-nav-bar-dropDownMenuItem-disconnect")
        .click();
      await expect(connectButton).toBeVisible();
      await openTrade(page);
      await expect(connectButton).toBeVisible();
    } else {
      await expect(connectButton).toBeVisible();
    }
    expect(
      await page.evaluate(() => (window as any).__nativeTestWrites),
    ).toEqual([]);
    expect(errors).toEqual([]);
  });
}

test("native market search selects the paxeer-network symbol", async ({
  page,
}) => {
  await openTrade(page);
  await page.getByPlaceholder("Search market", { exact: true }).fill("XAU");
  await page.getByText("XAU", { exact: true }).first().click();
  await expect(page).toHaveURL(/symbol=PERP_XAU_USDC/);
});

test("native grid panels drag and preserve their position", async ({
  page,
}) => {
  await openTrade(page, "/trade/grid");
  const book = page
    .locator(".react-grid-item")
    .filter({ has: page.getByText("Order book", { exact: true }) })
    .first();
  await expect
    .poll(async () => (await book.boundingBox())?.x ?? 0)
    .toBeGreaterThan(1000);
  const before = (await book.boundingBox())!;
  await page.mouse.move(before.x + 8, before.y + 8);
  await page.mouse.down();
  await page.mouse.move(before.x - 152, before.y + 64, { steps: 12 });
  await page.mouse.up();
  await expect
    .poll(async () =>
      book.evaluate((node, previousX) => {
        const box = node.getBoundingClientRect();
        const parent = node.parentElement!.getBoundingClientRect();
        const matrix = new DOMMatrixReadOnly(
          (node as HTMLElement).style.transform,
        );
        const targetX = parent.x + matrix.m41;
        const targetY = parent.y + matrix.m42;
        return (
          targetX < previousX - 60 &&
          Math.abs(box.x - targetX) < 1 &&
          Math.abs(box.y - targetY) < 1
        );
      }, before.x),
    )
    .toBe(true);
  const moved = (await book.boundingBox())!;
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect
    .poll(async () => Math.abs(((await book.boundingBox())?.x ?? 0) - moved.x))
    .toBeLessThan(8);
  await expect
    .poll(async () => Math.abs(((await book.boundingBox())?.y ?? 0) - moved.y))
    .toBeLessThan(8);
});

test("authenticated feeds render live prices, orderbook, trades and candles", async ({
  page,
}) => {
  const topics = new Set<string>();
  let historyBars = 0;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("websocket", (socket) => {
    if (new URL(socket.url()).pathname !== "/ws/eth") return;
    socket.on("framereceived", ({ payload }) => {
      try {
        const frame = JSON.parse(String(payload));
        if (frame.topic && frame.data) topics.add(frame.topic);
      } catch {
        /* Gateway control frames may be plain text. */
      }
    });
  });
  page.on("response", async (response) => {
    if (
      !response.url().includes("/api/price-feed/eth/history") ||
      response.status() !== 200
    )
      return;
    try {
      const body = await response.json();
      historyBars = body.c?.length || 0;
    } catch {
      /* The page can close after validation. */
    }
  });
  await openTrade(page);
  await expect
    .poll(
      () =>
        topics.has("ETH@perp_orderbook") &&
        topics.has("ETH@perp_trade") &&
        [...topics].some((topic) => topic.startsWith("ETH@kline_")),
      { timeout: 45000 },
    )
    .toBe(true);
  await expect.poll(() => historyBars, { timeout: 45000 }).toBeGreaterThan(0);
  await expect(
    page.getByText(/^\$[\d,]+\.\d+$/, { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByText(/NaN|Infinity/)).toHaveCount(0);
  expect(errors).toEqual([]);
});

test("Last trades renders history across market and tab switches", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await openTrade(page);
  const lastTrades = page.getByText("Last trades", { exact: true });
  await lastTrades.click();
  const tape = page.locator(".oui-lastTrades");
  const rows = tape.locator(".oui-last-trades-list .oui-grid");
  await expect(tape.getByText("Qty(ETH)", { exact: true })).toBeVisible();
  await expect(rows).toHaveCount(50);

  for (const symbol of ["BTC", "ETH"]) {
    await page.getByPlaceholder("Search market", { exact: true }).fill(symbol);
    await page.getByText(symbol, { exact: true }).first().click();
    await expect(page).toHaveURL(new RegExp(`symbol=PERP_${symbol}_USDC`));
    await expect(
      tape.getByText(`Qty(${symbol})`, { exact: true }),
    ).toBeVisible();
    await expect(rows).toHaveCount(50);
    await expect(rows.first()).toContainText(/\d{2}:\d{2}:\d{2}/);
    expect(
      Number(
        (
          await rows.first().locator(":scope > div").nth(1).innerText()
        ).replaceAll(",", ""),
      ),
    ).toBeGreaterThan(0);
    await page.getByText("Order book", { exact: true }).click();
    await expect(tape).not.toBeVisible();
    await lastTrades.click();
    await expect(rows).toHaveCount(50);
  }
  expect(errors).toEqual([]);
});

test("mobile Trades tab renders and reopens the live trade history", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/trade", { waitUntil: "domcontentloaded" });
  const topTabs = page.locator(".oui-trading-topTab");
  await topTabs.getByText("Trades", { exact: true }).click();
  const tape = topTabs.locator(".oui-lastTrades");
  await expect(tape.getByText("Qty(ETH)", { exact: true })).toBeVisible();
  await expect(tape.locator(".oui-last-trades-list .oui-grid")).toHaveCount(50);
  await topTabs.getByText("Data", { exact: true }).click();
  await expect(tape).not.toBeVisible();
  await topTabs.getByText("Trades", { exact: true }).click();
  await expect(tape.locator(".oui-last-trades-list .oui-grid")).toHaveCount(50);
  expect(errors).toEqual([]);
});
