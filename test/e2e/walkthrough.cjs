// End-to-end walkthrough of the web UI in a real (headless) browser.
//
// It expects rbd and rbweb already running as a simulated device on
// http://127.0.0.1:8080 (run.sh starts them). It then acts like a person
// with a phone: finishes setup, checks the status page, changes a setting,
// logs out and tries a wrong password.
//
// Steps find elements by their visible text and labels (like a person or a
// screen reader would), so a redesigned UI keeps passing as long as it
// keeps the wording listed in docs/ui-contract.md.

const { chromium } = require("playwright");

const BASE_URL = process.env.RASBEACON_URL ?? "http://127.0.0.1:8080";
const SCREENSHOT_DIR = process.env.SCREENSHOT_DIR ?? "";

async function main() {
  const browser = await chromium.launch(
    process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {},
  );
  // Phone-sized screen (iPhone 14/15).
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));

  const step = async (name, action) => {
    process.stdout.write(`- ${name} ... `);
    await action();
    if (SCREENSHOT_DIR) {
      await page.screenshot({ path: `${SCREENSHOT_DIR}/${name.replace(/\W+/g, "-")}.png`, fullPage: true });
    }
    console.log("ok");
  };

  await step("setup page opens", async () => {
    await page.goto(BASE_URL + "/");
    await page.getByText("Set up RasBeacon").waitFor();
  });

  await step("mismatched admin passwords are caught", async () => {
    await page.getByLabel("Admin password", { exact: true }).fill("tangerine-orbit-42");
    await page.getByLabel("Type it again").fill("different");
    await page.getByRole("button", { name: "Finish setup" }).click();
    await page.getByText("The two passwords don't match.").waitFor();
  });

  await step("setup completes", async () => {
    await page.getByLabel("Type it again").fill("tangerine-orbit-42");
    await page.getByRole("button", { name: "Finish setup" }).click();
    await page.getByText("Setup complete").waitFor();
  });

  await step("status page lists connected devices", async () => {
    // rbd switches to the owner's hotspot 5 seconds after setup.
    await page.waitForTimeout(6000);
    await page.goto(BASE_URL + "/#status");
    await page.reload();
    await page.getByText("Connected devices (2)").waitFor();
  });

  await step("hotspot settings validate and save", async () => {
    await page.getByRole("button", { name: "Hotspot" }).click();
    await page.getByLabel("Band").selectOption("5GHz");
    const passwordBox = page.getByLabel("New Wi-Fi password (leave empty to keep the current one)");
    await passwordBox.fill("short");
    await page.getByRole("button", { name: "Save" }).click();
    await page.getByText("Must be 8 to 63 characters long.").waitFor();
    await passwordBox.fill("");
    await page.getByRole("button", { name: "Save" }).click();
    await page.getByText("Saved. The hotspot has restarted.").waitFor();
  });

  await step("log out, then a wrong password is refused", async () => {
    await page.getByRole("button", { name: "System" }).click();
    await page.getByRole("button", { name: "Log out" }).click();
    // Wait for the login screen: the System page also has an "Admin
    // password" box (for factory reset), which must not be the one we fill.
    await page.getByText("Forgot the password?").waitFor();
    await page.getByLabel("Admin password").fill("wrong-password");
    await page.getByRole("button", { name: "Log in" }).click();
    await page.getByText("Wrong password.").waitFor();
  });

  await step("the right password logs in", async () => {
    await page.getByLabel("Admin password").fill("tangerine-orbit-42");
    await page.getByRole("button", { name: "Log in" }).click();
    await page.getByRole("heading", { name: "Status" }).waitFor();
  });

  await browser.close();
  if (pageErrors.length > 0) {
    throw new Error("JavaScript errors on the page: " + pageErrors.join("; "));
  }
  console.log("All steps passed.");
}

main().catch((error) => {
  console.error("\nFAILED:", error.message);
  process.exit(1);
});
