const { test, expect } = require('@playwright/test');

// One serial flow through the real UI: the create chooser + modal, inline edit, move via the
// dropdown, the worker-result panel, archive (kept) and delete (gone), the planning-lane +,
// and the board-context panel. Shares one board across tests (workers: 1), so order matters.
test.describe.configure({ mode: 'serial' });

const close = (page) => page.click('.sheet-h .x[data-x]');

test('create via chooser + modal, then inline-edit', async ({ page }) => {
  await page.goto('/');
  await page.click('#add-new');
  await expect(page.locator('.sheet-t')).toContainText('What do you want to start');
  await page.click('[data-new="ticket"]');
  await page.fill('#nt-title', 'Make a turtle game');
  await page.fill('#nt-body', 'ascii ninja turtles');
  await page.click('#nt-go');
  // landed on the new card
  await expect(page.locator('.sheet-t')).toContainText('Make a turtle game');
  await expect(page.locator('#md')).toContainText('ascii ninja turtles');

  // inline edit: title + body in place; Add-photo button is still present (no stripped view)
  await page.click('#edit');
  await expect(page.locator('#ed-title')).toBeVisible();
  await expect(page.locator('#sheet')).toContainText('Add photo/file');
  await page.fill('#ed-title', 'Turtle game v2');
  await page.fill('#ed-body', 'now with shells');
  await page.click('#ed-save');
  await expect(page.locator('.sheet-t')).toContainText('Turtle game v2');
  await close(page);
});

test('move a card with the lane dropdown', async ({ page }) => {
  await page.goto('/');
  await page.locator('.card', { hasText: 'Turtle game v2' }).click();
  await page.selectOption('#msel', 'ready');
  await page.locator('#msel').dispatchEvent('change');
  // the move reloads the board; the card is now in the ready lane
  await expect(page.locator('.card[data-lane="ready"]', { hasText: 'Turtle game v2' })).toBeVisible();
});

test('worker result shows as a red blocked panel', async ({ page, request }) => {
  // set a result like an agent would, via MCP
  const id = await page.goto('/').then(() =>
    page.locator('.card[data-lane="ready"]').first().getAttribute('data-id'));
  await request.post('/mcp', {
    headers: { 'Content-Type': 'application/json' },
    data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: {
      name: 'set_result', arguments: { id, status: 'blocked', summary: 'stuck on the canvas API', notes: ['need a sprite sheet'] } } },
  });
  await page.reload();
  await page.locator(`.card[data-id="${id}"]`).click();
  await expect(page.locator('.result.blocked')).toContainText('stuck on the canvas API');
  await expect(page.locator('.result.blocked')).toContainText('need a sprite sheet');
  await close(page);
});

test('archive keeps it off the board; delete removes it', async ({ page }) => {
  // archive the turtle card
  await page.goto('/');
  await page.locator('.card', { hasText: 'Turtle game v2' }).click();
  await page.click('#arch');
  await page.click('#c-ok'); // confirm "Archive"
  await expect(page.locator('.card', { hasText: 'Turtle game v2' })).toHaveCount(0);

  // create a throwaway, then delete it for good
  await page.click('#add-new');
  await page.click('[data-new="ticket"]');
  await page.fill('#nt-title', 'Delete me');
  await page.click('#nt-go');
  await close(page);
  await page.locator('.card', { hasText: 'Delete me' }).click();
  await page.click('#del');
  await page.click('#c-ok'); // confirm "Delete forever"
  await expect(page.locator('.card', { hasText: 'Delete me' })).toHaveCount(0);
});

test('planning lane + opens the chooser', async ({ page }) => {
  await page.goto('/');
  await page.locator('[data-newlane]').click();
  await expect(page.locator('.sheet-t')).toContainText('What do you want to start');
  await close(page);
});

test('run-worker nudge appears in Ready with the connect command', async ({ page }) => {
  await page.goto('/');
  await page.click('#add-new');
  await page.click('[data-new="ticket"]');
  await page.fill('#nt-title', 'Workable');
  await page.click('#nt-go');
  await page.selectOption('#msel', 'ready');
  await page.locator('#msel').dispatchEvent('change');
  await expect(page.locator('.card[data-lane="ready"]', { hasText: 'Workable' })).toBeVisible();
  await close(page); // the move reopened the card sheet; close it before clicking the board
  await page.locator('[data-runworker]').click();
  await expect(page.locator('.sheet-t')).toContainText('Run the worker');
  await expect(page.locator('#sheet')).toContainText('claude mcp add');
});

test('board context round-trips through the panel', async ({ page }) => {
  await page.goto('/');
  await page.click('#ctx');
  await page.click('#ctx-edit');
  await page.fill('#ctx-ta', '# Repos\n- app: ~/code/app');
  await page.click('#ctx-save');
  await expect(page.locator('#ctx-md')).toContainText('app: ~/code/app');
});
