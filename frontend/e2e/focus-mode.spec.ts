import { test, expect } from './fixtures';
import { FocusPage } from './pages/focus-page';

test.describe('Focus mode', () => {
  test('enters via the Focus button, hides the clutter and exits with Esc', async ({
    beans,
    backlogPage,
    page
  }) => {
    const id = beans.create('Read me', { status: 'todo' });
    beans.run(['update', id, '--tag', 'docs', '--body', '## Intro\n\nSome **bold** text.']);
    beans.create('Other bean', { status: 'todo' });
    const focus = new FocusPage(page);

    await backlogPage.goto(2);
    await backlogPage.selectBean('Read me');
    const enter = focus.enterButton();
    await expect(enter).toHaveAttribute('title', /\(F\)/);
    await enter.click();

    await focus.expectShowing('Read me', id);
    await expect(focus.meta).toContainText(id);
    await expect(focus.meta).toContainText('todo');
    await expect(focus.meta).toContainText('task');
    await expect(focus.meta).toContainText('#docs');
    await expect(focus.body.getByRole('heading', { name: 'Intro' })).toBeVisible();
    await expect(focus.body.locator('strong')).toHaveText('bold');
    // The article has the keyboard; the regular UI is hidden and inert.
    await expect(focus.article).toBeFocused();
    await expect(page.getByRole('button', { name: '+ New Bean' })).toBeHidden();
    await expect(backlogPage.beanByTitle('Other bean')).toBeHidden();
    await expect(page).toHaveTitle(/^Read me — /);

    await page.keyboard.press('Escape');
    await focus.expectClosed();
    await expect(page).toHaveURL(/[?&]bean=/);
    await expect(backlogPage.beanByTitle('Other bean')).toBeVisible();
    // Focus returns to where it was when entering.
    await expect(enter).toBeFocused();
  });

  test('enters with f, survives a reload and browser back exits', async ({
    beans,
    backlogPage,
    page
  }) => {
    const id = beans.create('Shortcut bean', { status: 'todo' });
    const focus = new FocusPage(page);

    await backlogPage.goto(1);

    // Typing f in the filter doesn't enter focus mode.
    await page.keyboard.press('ControlOrMeta+f');
    await page.keyboard.type('f');
    await expect(focus.view).toHaveCount(0);
    await page.getByRole('combobox').fill('');
    await page.getByRole('combobox').blur();

    // Nothing selected: f does nothing.
    await page.keyboard.press('f');
    await expect(focus.view).toHaveCount(0);

    await backlogPage.selectBean('Shortcut bean');
    await page.keyboard.press('f');
    await focus.expectShowing('Shortcut bean', id);

    await page.reload();
    await focus.expectShowing('Shortcut bean', id);
    await expect(focus.article).toBeFocused();

    await page.goBack();
    await focus.expectClosed();
    await expect(page).toHaveURL(/\/planning/);
    await backlogPage.waitForBean('Shortcut bean');

    // Forward re-enters (the URL is the state).
    await page.goForward();
    await focus.expectShowing('Shortcut bean', id);
  });

  test('a direct link opens focus mode and Back leaves it in place', async ({
    beans,
    backlogPage,
    page
  }) => {
    const id = beans.create('Linked bean', { status: 'todo' });
    const focus = new FocusPage(page);

    await page.goto(`${beans.baseURL}/planning?focus=${id}`);
    await focus.expectShowing('Linked bean', id);
    await focus.backButton.click();
    await focus.expectClosed();
    await backlogPage.waitForBean('Linked bean');

    // An unknown bean exits with a toast.
    await page.goto(`${beans.baseURL}/planning?focus=nope-1234`);
    await expect(page.locator('[data-toast]')).toContainText('Bean "nope-1234" not found');
    await focus.expectClosed();
  });

  test('prev/next steps through the filtered backlog in display order', async ({
    beans,
    backlogPage,
    page
  }) => {
    const ids: Record<string, string> = {};
    for (const title of ['Delta', 'Alpha', 'Charlie', 'Bravo', 'Echo']) {
      ids[title] = beans.create(title, { status: 'todo' });
    }
    const draft = beans.create('Foxtrot', { status: 'draft' });
    for (const title of ['Alpha', 'Charlie', 'Delta']) {
      beans.run(['update', ids[title], '--tag', 'ui']);
    }
    beans.run(['update', draft, '--tag', 'ui']);
    const focus = new FocusPage(page);

    await page.goto(`${beans.baseURL}/planning?q=${encodeURIComponent('tag:ui')}`);
    await expect(backlogPage.beanItems).toHaveCount(4);
    const order = await backlogPage.getBeanTitles();
    expect(order).toEqual(['Alpha', 'Charlie', 'Delta', 'Foxtrot']);

    await backlogPage.selectBean('Alpha');
    await focus.enterButton().click();
    await focus.expectShowing('Alpha', ids.Alpha);
    await expect(focus.position).toHaveText('1 of 4 in Backlog');
    await expect(focus.prevButton).toBeDisabled();
    // The filter stays in the URL.
    await expect(page).toHaveURL(/[?&]q=tag%3Aui/);

    await page.keyboard.press('ArrowRight');
    await focus.expectShowing('Charlie', ids.Charlie);
    await page.keyboard.press('j');
    await focus.expectShowing('Delta', ids.Delta);
    await focus.nextButton.click();
    await focus.expectShowing('Foxtrot', draft);
    await expect(focus.position).toHaveText('4 of 4 in Backlog');
    await expect(focus.nextButton).toBeDisabled();
    await page.keyboard.press('ArrowRight');
    await focus.expectShowing('Foxtrot', draft);

    await page.keyboard.press('k');
    await focus.expectShowing('Delta', ids.Delta);
    await page.keyboard.press('ArrowLeft');
    await focus.expectShowing('Charlie', ids.Charlie);

    // Stepping replaces the history entry: one back leaves focus mode.
    await page.goBack();
    await focus.expectClosed();
    await expect(page).toHaveURL(/[?&]q=tag%3Aui/);
    // The last focused bean is selected.
    await expect(page).toHaveURL(new RegExp(`[?&]bean=${ids.Charlie}`));
  });

  test('prev/next follows the board order when opened from the board', async ({
    beans,
    boardPage,
    page
  }) => {
    const todo = beans.create('Todo card', { status: 'todo' });
    const doing = beans.create('Doing card', { status: 'in-progress' });
    const focus = new FocusPage(page);

    await boardPage.goto();
    await page.getByText('Todo card').click();
    await page.keyboard.press('f');
    await focus.expectShowing('Todo card', todo);
    await expect(focus.position).toHaveText('1 of 2 in Board');
    await page.keyboard.press('ArrowRight');
    await focus.expectShowing('Doing card', doing);
    await page.keyboard.press('Escape');
    await focus.expectClosed();
    await expect(page).toHaveURL(/\/planning\/board/);
  });

  test('related links navigate within focus mode', async ({ beans, page }) => {
    const epic = beans.create('The Epic', { status: 'todo', type: 'epic' });
    const task = beans.create('The Task', { status: 'todo' });
    // Bodies link the IDs of existing beans, whatever the project's prefix.
    const blocker = beans.create('The Blocker', { status: 'todo' });
    beans.run(['update', task, '--parent', epic, '--blocked-by', blocker]);
    beans.run(['update', epic, '--body', `See ${blocker} for details.`]);
    const focus = new FocusPage(page);

    await page.goto(`${beans.baseURL}/planning?focus=${task}`);
    await focus.expectShowing('The Task', task);
    await expect(focus.related.getByRole('heading', { name: 'Related' })).toBeVisible();
    await expect(focus.relatedLink(epic)).toContainText('The Epic');
    await expect(focus.relatedLink(blocker)).toContainText('The Blocker');
    await expect(focus.meta.locator('[data-focus-parent]')).toHaveText('The Epic');

    // Parent link (in the metadata line)
    await focus.meta.locator('[data-focus-parent]').click();
    await focus.expectShowing('The Epic', epic);
    await expect(focus.article).toBeFocused();
    await expect(focus.relatedLink(task)).toContainText('The Task');

    // Bean link in the body
    await focus.body.locator(`[data-bean-id="${blocker}"]`).click();
    await focus.expectShowing('The Blocker', blocker);
    // Blocking is shown from the blocked bean's link (symmetric)
    await expect(focus.relatedLink(task)).toContainText('The Task');

    await focus.relatedLink(task).click();
    await focus.expectShowing('The Task', task);

    // Links are real hrefs (open in a new tab, bookmark, …).
    await expect(focus.relatedLink(epic)).toHaveAttribute('href', new RegExp(`focus=${epic}`));

    await page.keyboard.press('Escape');
    await focus.expectClosed();
    await expect(page.locator(`.bean-item[data-bean-id="${task}"]`)).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`[?&]bean=${task}`));
  });

  test('live updates from the CLI and exits when the bean is deleted', async ({
    beans,
    backlogPage,
    page
  }) => {
    const id = beans.create('Live bean', { status: 'todo' });
    beans.run(['update', id, '--body', 'First version.\n\n- [x] done item\n- [ ] open item']);
    const focus = new FocusPage(page);

    await backlogPage.goto(1);
    await backlogPage.selectBean('Live bean');
    await page.keyboard.press('f');
    await focus.expectShowing('Live bean', id);
    await expect(focus.body).toContainText('First version.');

    // Task list checkboxes render like in the detail pane (read-only there too).
    const boxes = focus.body.locator('input[type="checkbox"]');
    await expect(boxes).toHaveCount(2);
    await expect(boxes.nth(0)).toBeChecked();
    await expect(boxes.nth(1)).not.toBeChecked();

    beans.run(['update', id, '--body', 'Second version.', '--title', 'Live bean v2']);
    await expect(focus.body).toContainText('Second version.');
    await expect(focus.title).toHaveText('Live bean v2');

    beans.run(['delete', '--force', id]);
    await expect(page.locator('[data-toast]')).toContainText('"Live bean v2" was deleted');
    await focus.expectClosed();
  });

  test('Edit opens the edit form and the reading width toggles', async ({
    beans,
    backlogPage,
    page
  }) => {
    const id = beans.create('Editable bean', { status: 'todo' });
    const focus = new FocusPage(page);

    await page.goto(`${beans.baseURL}/planning?focus=${id}`);
    await focus.expectShowing('Editable bean', id);

    await focus.editButton.click();
    await expect(page.getByRole('heading', { name: 'Edit Bean' })).toBeVisible();
    // Esc doesn't leave focus mode while the form is open.
    await page.keyboard.press('Escape');
    await expect(focus.view).toBeVisible();
    await page.getByRole('button', { name: 'Cancel' }).click();
    await expect(page.getByRole('heading', { name: 'Edit Bean' })).toHaveCount(0);
    await expect(focus.article).toBeFocused();

    await page.keyboard.press('e');
    await expect(page.getByRole('heading', { name: 'Edit Bean' })).toBeVisible();
    await page.getByRole('button', { name: 'Cancel' }).click();

    await expect(focus.widthToggle).toHaveAttribute('aria-pressed', 'false');
    const narrow = (await focus.article.boundingBox())!.width;
    await page.keyboard.press('w');
    await expect(focus.widthToggle).toHaveAttribute('aria-pressed', 'true');
    await expect
      .poll(async () => (await focus.article.boundingBox())!.width)
      .toBeGreaterThan(narrow);
    await page.reload();
    await expect(focus.widthToggle).toHaveAttribute('aria-pressed', 'true');
    await focus.widthToggle.click();
    await expect(focus.widthToggle).toHaveAttribute('aria-pressed', 'false');

    await page.keyboard.press('Escape');
    await focus.expectClosed();
    await backlogPage.waitForBean('Editable bean');
  });
});
