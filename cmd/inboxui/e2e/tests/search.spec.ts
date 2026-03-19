import { test, expect } from "@playwright/test";

test.describe("Search", () => {
  test("finds items by text", async ({ page }) => {
    await page.goto("/search");
    await page.getByPlaceholder("Search inbox items").fill("screening");
    await page.getByRole("button", { name: "Search" }).click();
    await expect(page.getByText("PEP screening review")).toBeVisible();
  });

  test("shows no results for non-matching query", async ({ page }) => {
    await page.goto("/search");
    await page.getByPlaceholder("Search inbox items").fill("xyznonexistent");
    await page.getByRole("button", { name: "Search" }).click();
    await expect(page.getByText("No items found")).toBeVisible();
  });

  test("clicking result opens detail drawer", async ({ page }) => {
    await page.goto("/search");
    await page.getByPlaceholder("Search inbox items").fill("invoice");
    await page.getByRole("button", { name: "Search" }).click();
    await page.getByText("Invoice approval").click();
    await expect(page.locator("#drawer-panel")).toBeVisible();
  });
});
