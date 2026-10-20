// End-to-end walkthrough of the web UI in a real (headless) browser.
//
// It expects rbd and rbweb already running as a simulated device on
// http://127.0.0.1:8080 (run.sh starts them). It then acts like a person
// with a phone: finishes setup, checks the status page, changes settings on
// every page, uses the VPN profiles, the per-device controls and the speed
// test, changes the admin password, logs out and tries the old one.
//
// Steps find elements by their role and visible name (like a person or a
// screen reader would), so a restyled UI keeps passing as long as it keeps
// the wording listed in docs/ui-contract.md.
//
// E2E_VIEWPORT=desktop runs it on a computer-sized screen (the sidebar
// layout) instead of a phone's (the tab bar); CI runs both.

const { chromium } = require("playwright");
const fs = require("fs");
const os = require("os");
const path = require("path");

const BASE_URL = process.env.RASBEACON_URL ?? "http://127.0.0.1:8080";
const SCREENSHOT_DIR = process.env.SCREENSHOT_DIR ?? "";
// The simulated hotel login page (run.sh sets this when a headless
// Chromium is available for the simulated device's own browser, which the
// zero-leak login and the speed test need).
const HOTEL_URL = process.env.RASBEACON_E2E_HOTEL ?? "";
const DESKTOP = process.env.E2E_VIEWPORT === "desktop";
// Screenshots of each screen size are kept apart.
const PREFIX = DESKTOP ? "desktop-" : "phone-";

// A WireGuard file with the WireGuard documentation's example keys.
const WIREGUARD_CONF = `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.66.0.2/32
DNS = 10.66.0.1

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0, ::/0
`;

// Pages that phones reach through "More" (computers have them in the sidebar).
const ON_MORE_PAGE = new Set(["Hotspot", "Guests", "Plugins", "SSH", "HTTPS", "System"]);

async function main() {
  const browser = await chromium.launch(
    process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {},
  );
  // A phone (iPhone 14/15) or a laptop.
  const page = await browser.newPage({ viewport: DESKTOP ? { width: 1280, height: 800 } : { width: 390, height: 844 } });
  console.log(`Screen: ${DESKTOP ? "desktop, 1280 x 800" : "phone, 390 x 844"}`);
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));

  const step = async (name, action) => {
    process.stdout.write(`- ${name} ... `);
    try {
      await action();
    } catch (error) {
      if (SCREENSHOT_DIR) {
        await page.screenshot({ path: `${SCREENSHOT_DIR}/${PREFIX}FAILED-${name.replace(/\W+/g, "-")}.png`, fullPage: true });
      }
      throw error;
    }
    if (SCREENSHOT_DIR) {
      await page.screenshot({ path: `${SCREENSHOT_DIR}/${PREFIX}${name.replace(/\W+/g, "-")}.png`, fullPage: true });
    }
    console.log("ok");
  };

  const nav = page.getByRole("navigation", { name: "Main navigation" });
  const main = page.getByRole("main");
  // Opens a page the way a person would: from the sidebar, the tab bar, or
  // through "More".
  const open = async (name) => {
    if (!DESKTOP && ON_MORE_PAGE.has(name)) {
      await nav.getByRole("link", { name: "More", exact: true }).click();
      await main.getByRole("link", { name, exact: true }).click();
    } else {
      await nav.getByRole("link", { name, exact: true }).click();
    }
    await page.getByRole("heading", { level: 1, name, exact: true }).waitFor();
  };
  // A dialog (sheet or alert) on the page.
  const dialog = () => page.getByRole("dialog");
  // The Internet page's network list saves from its floating bar (the page has other Save buttons).
  const saveNetworks = () =>
    page.getByRole("region", { name: "Unsaved changes" }).getByRole("button", { name: "Save", exact: true }).click();

  await step("setup page opens", async () => {
    await page.goto(BASE_URL + "/");
    await page.getByRole("heading", { name: "Set up RasBeacon" }).waitFor();
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
    await page.getByRole("heading", { name: "Setup complete" }).waitFor();
  });

  await step("status page lists connected devices", async () => {
    // rbd switches to the owner's hotspot 5 seconds after setup.
    await page.waitForTimeout(6000);
    await page.goto(BASE_URL + "/#/");
    await page.reload();
    await page.getByRole("heading", { name: "Connected devices (2)" }).waitFor();
  });

  await step("status page shows the internet connection and the hardware", async () => {
    await page.getByText('Connected to Wi-Fi "Corner Café"').waitFor();
    await page.getByRole("meter", { name: "Memory" }).waitFor();
  });

  await step("internet page finds nearby networks and saves one", async () => {
    await open("Internet");
    await page.getByRole("button", { name: "Look for nearby networks" }).click();
    await page.getByRole("button", { name: "Hotel Guest" }).click();
    await dialog().getByRole("button", { name: "Add to list" }).click();
    await page.getByText("You have changes that are not saved yet.").waitFor();
    await saveNetworks();
    await page.getByText("Saved. RasBeacon is joining the networks now").waitFor();
  });

  await step("a WPA-Enterprise network saves without a CA certificate (trusted at the first connection)", async () => {
    await page.getByRole("button", { name: "Add manually" }).click();
    const sheet = dialog();
    await sheet.getByLabel("Network name").fill("Campus Wi-Fi");
    await sheet.getByLabel("Security").selectOption("enterprise");
    await sheet.getByLabel("Username", { exact: true }).fill("NetID@example.edu");
    await sheet.getByLabel("Your password").fill("campus-password");
    await sheet.getByRole("button", { name: "Add to list" }).click();
    await saveNetworks();
    await page.getByText("Trusted at the first connection").waitFor();
  });

  await step("a WPA-Enterprise network (eduroam) with its CA certificate under Advanced", async () => {
    await page.getByRole("button", { name: "Add manually" }).click();
    const sheet = dialog();
    await sheet.getByLabel("Network name").fill("eduroam");
    await sheet.getByLabel("Security").selectOption("enterprise");
    await sheet.getByLabel("Username", { exact: true }).fill("NetID@example.edu");
    await sheet.getByLabel("Your password").fill("campus-password");
    await sheet.getByText("Advanced", { exact: true }).click();
    // A test certificate (fixtures/test-campus-ca.pem; certificate only, no key).
    await sheet.getByLabel(/^CA certificate file/).setInputFiles(__dirname + "/fixtures/test-campus-ca.pem");
    await sheet.getByLabel("Login server name").fill("*.example.edu");
    await sheet.getByRole("button", { name: "Add to list" }).click();
    await saveNetworks();
    // The router reads the certificate and describes it.
    await page.getByText("RasBeacon Test Campus CA (valid until").waitFor();
  });

  await step("internet page renews the lease", async () => {
    await page.getByRole("button", { name: "Renew lease" }).click();
    await page.getByRole("button", { name: "Renew lease" }).waitFor();
  });

  if (HOTEL_URL) {
    await step("the speed test runs when asked and shows its result", async () => {
      await page.getByRole("button", { name: "Run speed test" }).click();
      // The simulated device tests against a local stand-in for Cloudflare.
      await page.getByText("Simulated city (DEV)").waitFor({ timeout: 120000 });
      await main.getByText("Download", { exact: true }).first().waitFor();
    });
  }

  await step("VPN: an unsafe file is refused, a good one becomes a profile", async () => {
    await open("VPN");
    const paste = page.getByLabel("Or paste its contents");
    await paste.fill(WIREGUARD_CONF.replace("DNS =", "PostUp = echo hi\nDNS ="));
    await page.getByRole("button", { name: "Import", exact: true }).click();
    await page.getByText("never runs commands").waitFor();
    await paste.fill(WIREGUARD_CONF);
    await page.getByRole("button", { name: "Import", exact: true }).click();
    await page.getByText("WireGuard file imported").waitFor();
    await page.getByRole("radio", { name: /wireguard\.conf/ }).waitFor();
  });

  await step("VPN: the first profile turns it on and protects the devices", async () => {
    await page.getByRole("switch", { name: "Use the VPN", checked: true }).waitFor();
    await open("Status");
    await page.getByText("Connected: your devices' traffic goes through the VPN.").waitFor();
  });

  await step("VPN: a second profile can be chosen, then removed", async () => {
    await open("VPN");
    await page.getByLabel("WireGuard file", { exact: true }).setInputFiles({
      name: "office.conf",
      mimeType: "text/plain",
      buffer: Buffer.from(WIREGUARD_CONF.replace("vpn.example.com", "office.example.com")),
    });
    await page.getByRole("button", { name: "Import", exact: true }).click();
    const office = page.getByRole("radio", { name: /office\.conf/ });
    await office.click();
    await page.getByRole("radio", { name: /office\.conf/, checked: true }).waitFor();
    await page.getByRole("button", { name: "Remove office.conf" }).click();
    await dialog().getByRole("button", { name: "Remove", exact: true }).click();
    await office.waitFor({ state: "detached" });
    await page.getByRole("radio", { name: /wireguard\.conf/, checked: true }).waitFor();
  });

  await step("VPN: the server details check numbers and save", async () => {
    await page.getByText("Server details (advanced)").click();
    const mtu = page.getByLabel(/^MTU/);
    await mtu.fill("big");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.getByText("Must be a number, or empty for the default.").waitFor();
    await mtu.fill("1420");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.getByText("Saved.", { exact: true }).waitFor();
    await page.getByRole("button", { name: "Generate new key" }).click();
    await dialog().getByRole("button", { name: "Generate", exact: true }).click();
    await page.getByRole("button", { name: "Generate new key" }).waitFor();
  });

  await step("the login-page notice opens without logging in", async () => {
    // The simulated network has no login page, so there is nothing to do.
    const other = await browser.newPage({ viewport: { width: 390, height: 844 } });
    await other.goto(BASE_URL + "/#/portal");
    await other.getByRole("heading", { name: "This network needs a login" }).waitFor();
    await other.getByText("Nothing to do").waitFor();
    await other.close();
  });

  if (HOTEL_URL) {
    await step("zero-leak login: the hotel's page opens in RasBeacon's own browser", async () => {
      // From now on the simulated network shows a login page.
      await fetch(HOTEL_URL + "/require-login");
      const phone = await browser.newPage({ viewport: { width: 390, height: 844 } });
      phone.on("pageerror", (error) => pageErrors.push(error.message));
      await phone.goto(BASE_URL + "/#/portal");
      await phone.getByRole("button", { name: "Check again" }).click();
      await phone.getByRole("button", { name: "Open the login page here" }).click({ timeout: 20000 });
      await phone.getByText("Grand Hotel Wi-Fi", { exact: true }).waitFor({ timeout: 20000 });
      if (SCREENSHOT_DIR) await phone.screenshot({ path: `${SCREENSHOT_DIR}/zero-leak-login.png` });

      // Tap the room field on the picture (it sits at 24..324 x 120..160 on
      // the 390-pixel-wide page), type the room number, press Enter.
      const picture = phone.getByRole("img", { name: /login page/ });
      const box = await picture.boundingBox();
      const natural = await picture.evaluate((img) => ({ w: img.naturalWidth, h: img.naturalHeight }));
      await phone.mouse.click(box.x + (170 * box.width) / natural.w, box.y + (140 * box.height) / natural.h);
      await phone.getByLabel("Tap a field on the page, then type here").fill("412");
      await phone.getByRole("button", { name: "Type it" }).click();
      await phone.getByRole("button", { name: "Enter" }).click();

      // The hotel lets RasBeacon in; the next check notices and the
      // browser closes by itself.
      await phone.getByText("Nothing to do").waitFor({ timeout: 30000 });
      await phone.close();
    });
  }

  await step("clients: a device's internet access is paused, then allowed again", async () => {
    await open("Clients");
    const access = page.getByRole("switch", { name: "Internet access for Pixel-8" });
    await access.click();
    await page.getByRole("switch", { name: "Internet access for Pixel-8", checked: false }).waitFor();
    await page.getByRole("radio", { name: "Paused (1)" }).waitFor();
    await access.click();
    await page.getByRole("switch", { name: "Internet access for Pixel-8", checked: true }).waitFor();
  });

  await step("clients: a device can be renamed, and the data limit saved", async () => {
    const card = page.getByRole("switch", { name: "Internet access for Pixel-8" }).locator("xpath=ancestor::section[1]");
    await card.getByRole("button", { name: "Rename" }).click();
    await dialog().getByRole("textbox", { name: "Name" }).fill("Ali's phone");
    await dialog().getByRole("button", { name: "Save" }).click();
    await page.getByRole("switch", { name: "Internet access for Ali's phone" }).waitFor();
    await page.getByText("Calls itself").first().waitFor();
    await page.getByLabel("Limit per device").selectOption("2000");
    const save = page.getByRole("button", { name: "Save", exact: true });
    await save.click();
    // Saved (it works without a metered connection now): the Save button goes away.
    await save.waitFor({ state: "detached" });
    await page.getByText("When a device uses up its data, its traffic is blocked").waitFor();
  });

  await step("HTTPS can be turned on and shows the certificate fingerprint", async () => {
    await open("HTTPS");
    await page.getByRole("switch", { name: "Use HTTPS for these settings pages" }).click();
    await page.getByText("HTTPS is on.").waitFor();
    await page.getByText("SHA-256 fingerprint").waitFor();
    // (The simulated device doesn't restart its web server, so this browser
    // stays on plain HTTP for the remaining steps.)
  });

  await step("hotspot settings validate and save", async () => {
    await open("Hotspot");
    await page.getByLabel("Band").selectOption("5GHz");
    const passwordBox = page.getByLabel("New Wi-Fi password (leave empty to keep the current one)");
    await passwordBox.fill("short");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.getByText("Must be 8 to 63 characters long.").waitFor();
    await passwordBox.fill("");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.getByText("Saved.", { exact: true }).waitFor();
  });

  await step("the current Wi-Fi password and a QR code are shown", async () => {
    await page.getByRole("button", { name: "Show password and QR code" }).click();
    // The password setup suggested (four groups of four letters and digits).
    await page.getByText(/^[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}$/).waitFor();
    await page.getByRole("img", { name: /^QR code to join / }).waitFor();
  });

  await step("who can join: the allow list saves and checks its addresses", async () => {
    await page.getByText("Allowed devices", { exact: true }).click();
    await page.getByLabel("Only allow these devices").check();
    await page.getByLabel(/^MAC addresses/).fill("3a:1f:22:aa:bb:cc");
    const save = page.getByRole("button", { name: "Save", exact: true }).last();
    await save.click();
    // Nobody is "this device" on the simulated device, so it saves; a bad address doesn't.
    await page.getByText("2 devices").or(page.getByText("1 device", { exact: true })).first().waitFor();
    await page.getByLabel(/^MAC addresses/).fill("not-a-mac");
    await save.click();
    await page.getByText(/is not a MAC address/).waitFor();
    await page.getByLabel("Only allow these devices").uncheck();
    await page.getByLabel(/^MAC addresses/).fill("");
    await save.click();
    await page.getByText("Anyone with the password").waitFor();
    await page.getByText("Allowed devices", { exact: true }).click(); // closed again
    // Its Save goes once the closing animation ends; until then the page has two.
    await page.getByLabel("Only allow these devices").waitFor({ state: "hidden" });
  });

  await step("the metered setting saves and shows on the status page", async () => {
    await page.getByLabel("Metered connection (for example a phone's hotspot or a paid plan)").check();
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.getByText("Saved.", { exact: true }).first().waitFor();
    await open("Status");
    await page.getByText(/Metered connection\s*On/).waitFor();
  });

  await step("the guest network needs a password, then runs", async () => {
    await open("Guests");
    await page.getByLabel("Turn on the guest network").check();
    await page.getByLabel("Guest network name").fill("Ali's Guests");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.getByText("Choose a password for the guest network.").waitFor();
    await page.getByLabel("Guest password").fill("visitors-welcome");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.getByText("Saved. The hotspot has restarted.").waitFor();
    await page.getByRole("button", { name: "Show QR code" }).click();
    await page.getByRole("img", { name: "QR code to join Ali's Guests" }).waitFor();
    await open("Status");
    await page.getByRole("heading", { name: "Guest network" }).waitFor();
    await page.getByText("Ali's Guests").waitFor();
  });

  await step("plugins: none yet, and a file that isn't a plugin is refused", async () => {
    await open("Plugins");
    await page.getByText("No plugins yet").waitFor();
    await page.getByLabel("Install from a file").setInputFiles({
      name: "not-a-plugin.rba",
      mimeType: "application/octet-stream",
      buffer: Buffer.from("hello"),
    });
    await page.getByText("This is not a RasBeacon plugin (.rba) file.").waitFor();
  });

  await step("system: a backup downloads, and restores with its password", async () => {
    await open("System");
    await page.getByRole("button", { name: "Back up settings" }).click();
    await dialog().getByLabel("Password for the backup").fill("backup-pass-42");
    const downloading = page.waitForEvent("download");
    await dialog().getByRole("button", { name: "Back up" }).click();
    const download = await downloading;
    if (!download.suggestedFilename().endsWith(".rbbackup")) throw new Error(`file name ${download.suggestedFilename()}`);
    const file = path.join(os.tmpdir(), `rasbeacon-e2e-${process.pid}.rbbackup`);
    await download.saveAs(file);

    await page.getByRole("button", { name: "Restore from a backup" }).click();
    await dialog().getByLabel("Backup file (.rbbackup)").setInputFiles(file);
    await dialog().getByLabel("The backup's password").fill("not-the-password");
    await dialog().getByRole("button", { name: "Restore" }).click();
    await dialog().getByText("The password is wrong, or the backup file is damaged.").waitFor();
    await dialog().getByLabel("The backup's password").fill("backup-pass-42");
    await dialog().getByRole("button", { name: "Restore" }).click();
    await page.getByText("RasBeacon is restarting with the backup's settings.", { exact: false }).waitFor();
    await dialog().getByRole("button", { name: "OK" }).click();
    fs.unlinkSync(file);
  });

  await step("system: renaming checks the name while it is typed", async () => {
    await open("System");
    await page.getByRole("button", { name: "Rename RasBeacon" }).click();
    await dialog().getByRole("textbox", { name: "Name" }).fill("my pi");
    await dialog().getByText("Use only lowercase letters, digits and hyphens.").waitFor();
    await dialog().getByRole("button", { name: "Save", disabled: true }).waitFor();
    await dialog().getByRole("button", { name: "Cancel" }).click();
  });

  await step("system: reboot, shut down and factory reset ask first", async () => {
    await page.getByRole("button", { name: "Reboot" }).click();
    await dialog().getByRole("button", { name: "Reboot" }).waitFor();
    await dialog().getByRole("button", { name: "Cancel" }).click();
    await page.getByRole("button", { name: "Shut down" }).click();
    await dialog().getByText("Shut down RasBeacon?").waitFor();
    await dialog().getByRole("button", { name: "Cancel" }).click();
    await page.getByText("Factory reset").click();
    await page.getByRole("button", { name: "Erase everything" }).waitFor();
  });

  await step("system: the admin password changes, given the current one", async () => {
    await page.getByRole("button", { name: "Change admin password" }).click();
    await dialog().getByLabel("Current password").fill("not-the-password");
    await dialog().getByLabel("New password").fill("plum-harbor-sunrise-7");
    await dialog().getByLabel("Type it again").fill("plum-harbor-sunrise-7");
    await dialog().getByRole("button", { name: "Change" }).click();
    await dialog().getByText("Wrong password.").waitFor();
    await dialog().getByLabel("Current password").fill("tangerine-orbit-42");
    await dialog().getByRole("button", { name: "Change" }).click();
    await page.getByText("Admin password changed.").waitFor();
  });

  await step("log out, then the old password is refused", async () => {
    await page.getByRole("button", { name: "Log out" }).click();
    // Wait for the login screen: the System page also has an "Admin
    // password" box (for factory reset), which must not be the one we fill.
    await page.getByText("Forgot the password?").waitFor();
    await page.getByLabel("Admin password").fill("tangerine-orbit-42");
    await page.getByRole("button", { name: "Log in" }).click();
    await page.getByText("Wrong password.").waitFor();
  });

  await step("the new password logs in, staying logged in", async () => {
    await page.getByLabel("Admin password").fill("plum-harbor-sunrise-7");
    await page.getByLabel("Stay logged in").check();
    await page.getByRole("button", { name: "Log in" }).click();
    await page.getByRole("heading", { level: 1, name: "Status" }).waitFor();
    const cookies = await page.context().cookies();
    if (!cookies.some((cookie) => cookie.name === "rasbeacon_remember")) {
      throw new Error('"Stay logged in" should leave a lasting login cookie');
    }
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
