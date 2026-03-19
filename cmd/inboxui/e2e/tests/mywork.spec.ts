import { test, expect } from "@playwright/test";

test.describe("My Work", () => {
  test("shows empty state when no claimed items", async ({ page }) => {
    await page.goto("/mywork?actor=user:new-user");
    await expect(page.getByText("no claimed items")).toBeVisible();
    await expect(page.getByText("Go to Queue")).toBeVisible();
  });

  test("shows claimed items after claiming", async ({ page }) => {
    // Claim an item first
    await page.goto("/?actor=user:test-worker");
    await page.getByText("Contract terms review").click();
    await page.locator("#drawer-panel").getByRole("button", { name: "Claim" }).click();

    // Navigate to My Work
    await page.getByRole("tab", { name: "My Work" }).click();
    await expect(page.getByText("Contract terms review")).toBeVisible();
  });
});
