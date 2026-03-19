import { test, expect } from "@playwright/test";

test.describe("Queue View", () => {
  test("shows seeded items", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator("table tbody tr")).toHaveCount(5);
    await expect(page.getByText("PEP screening review")).toBeVisible();
  });

  test("filters by priority", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("radio", { name: "urgent" }).click();
    await expect(page.getByText("PEP screening review")).toBeVisible();
  });

  test("clicking row opens detail drawer", async ({ page }) => {
    await page.goto("/");
    await page.getByText("PEP screening review").click();
    await expect(page.locator("#drawer-panel")).toBeVisible();
    await expect(
      page.locator("#drawer-panel").getByText("PEP screening review")
    ).toBeVisible();
  });
});
