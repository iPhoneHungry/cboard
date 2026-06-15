const { defineConfig } = require('@playwright/test');
const path = require('path');

// Drives the real dashboard in a headless browser. The webServer launches the cboard binary
// (built at the repo root first) against a throwaway board under a fake HOME, so the e2e run
// never touches a real board or ~/.cboard.json.
const HOME = path.join(__dirname, '.home');
const BIN = path.join(__dirname, '..', 'cboard');

module.exports = defineConfig({
  testDir: '.',
  fullyParallel: false,
  workers: 1,
  use: { baseURL: 'http://localhost:8788', trace: 'on-first-retry' },
  webServer: {
    // Wipe and re-seed the throwaway board immediately before serving — done in this command
    // (not a globalSetup) so the clean can never race with the server start.
    command: `rm -rf "${HOME}" && HOME="${HOME}" "${BIN}" serve "${path.join(HOME, 'board')}" --port 8788`,
    url: 'http://localhost:8788/',
    reuseExistingServer: false,
    timeout: 60_000,
  },
});
