# Cloudflare's speed test library

`speedtest.js` and `LICENSE` are copied unchanged from the npm package
[@cloudflare/speedtest](https://github.com/cloudflare/speedtest) version
1.14.1 (`dist/speedtest.js`), MIT license. internal/speedtest runs it in
RasBeacon's headless browser.

To update: download the new package (`npm pack @cloudflare/speedtest`),
copy `dist/speedtest.js` and `LICENSE` here, update the version above and
in internal/speedtest, and run `go test ./internal/speedtest/` (it checks
that the file still ends with the export RasBeacon replaces).
