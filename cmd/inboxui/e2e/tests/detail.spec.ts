import { test, expect } from "@playwright/test";

test.describe("Item Detail", () => {
  test("shows item metadata in drawer", async ({ page }) => {
    await page.goto("/");
    await page.getByText("PEP screening review").click();

    const drawer = page.locator("#drawer-panel");
    await expect(drawer).toBeVisible();
    await expect(drawer.getByText("compliance")).toBeVisible();
    await expect(drawer.getByText("urgent")).toBeVisible();
  });

  test("shows activity feed with creation event", async ({ page }) => {
    await page.goto("/");
    await page.getByText("PEP screening review").click();

    const drawer = page.locator("#drawer-panel");
    await expect(drawer.getByText("Created")).toBeVisible();
  });

  test("shows payload as JSON when no custom renderer", async ({ page }) => {
    await page.goto("/");
    await page.getByText("PEP screening review").click();

    const drawer = page.locator("#drawer-panel");
    await expect(drawer.getByText("Payload")).toBeVisible();
  });
});
