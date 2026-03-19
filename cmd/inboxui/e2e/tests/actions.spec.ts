import { test, expect } from "@playwright/test";

test.describe("Actions", () => {
  test("claim -> respond -> complete flow", async ({ page }) => {
    await page.goto("/?actor=user:operator");
    await page.getByText("Invoice approval").click();

    const drawer = page.locator("#drawer-panel");

    // Claim
    await drawer.getByRole("button", { name: "Claim" }).click();
    await expect(drawer.getByText("claimed")).toBeVisible();

    // Respond
    await drawer.getByRole("button", { name: "Respond" }).click();
    await drawer.getByPlaceholder("Action").fill("approve");
    await drawer.getByPlaceholder("Comment").fill("Looks good");
    await drawer.getByRole("button", { name: "Submit" }).click();
    await expect(drawer.getByText("Responded")).toBeVisible();

    // Complete
    await drawer.getByRole("button", { name: "Complete" }).click();
    await expect(drawer.getByText("completed")).toBeVisible();
  });

  test("cancel with reason", async ({ page }) => {
    await page.goto("/?actor=user:operator");
    await page.getByText("Expense report").click();

    const drawer = page.locator("#drawer-panel");
    await drawer.getByRole("button", { name: "Cancel" }).click();
    await drawer.getByPlaceholder("Reason").fill("Duplicate");
    await drawer.getByRole("button", { name: "Confirm Cancel" }).click();
    await expect(drawer.getByText("cancelled")).toBeVisible();
  });

  test("add comment", async ({ page }) => {
    await page.goto("/?actor=user:operator");
    await page.getByText("Missing customer data").click();

    const drawer = page.locator("#drawer-panel");
    await drawer.getByPlaceholder("Write a comment").fill("Contacted customer");
    await drawer.getByRole("button", { name: "Comment" }).click();
    await expect(drawer.getByText("Contacted customer")).toBeVisible();
  });
});
