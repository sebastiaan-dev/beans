import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect } from './fixtures';
import { agentSession } from './agent-session';

test.describe('Agent chat', () => {
  test('Bean IDs are autolinked in tool and info messages', async ({ page, beans }) => {
    // Only IDs of existing beans are linked (any prefix).
    const a = beans.create('Linked bean A');
    const b = beans.create('Linked bean B');
    await agentSession('__central__', beans)
      .withMessages([
        { role: 'user', content: 'update the bean' },
        { role: 'tool', content: `Bash: beans update ${a} -s in-progress` },
        { role: 'info', content: `Working on ${b}, not beans-zz99` },
        { role: 'assistant', content: `Done updating ${a}.` }
      ])
      .open(page);

    // Tool message should have an autolinked bean ID
    const toolBeanLink = page.locator(`.bean-link[data-bean-id="${a}"]`).first();
    await expect(toolBeanLink).toBeVisible({ timeout: 5000 });

    // Info message should have an autolinked bean ID
    const infoBeanLink = page.locator(`.bean-link[data-bean-id="${b}"]`);
    await expect(infoBeanLink).toBeVisible({ timeout: 5000 });

    // Assistant message should also have an autolinked bean ID (via renderMarkdown)
    const assistantBeanLink = page.locator(`.bean-link[data-bean-id="${a}"]`).last();
    await expect(assistantBeanLink).toBeVisible({ timeout: 5000 });

    // Unknown IDs are not linked
    await expect(page.locator('.bean-link[data-bean-id="beans-zz99"]')).toHaveCount(0);
  });

  test('Clear button resets the conversation in the UI', async ({ page, beans }) => {
    await agentSession('__central__', beans)
      .withMessages([
        { role: 'user', content: 'hello agent' },
        { role: 'assistant', content: 'Hi! How can I help?' }
      ])
      .open(page);

    // Verify the seeded messages are visible
    await expect(page.locator('text=hello agent')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('text=Hi! How can I help?')).toBeVisible({ timeout: 5000 });

    // Clear button should be enabled
    const clearBtn = page.locator('button:has-text("Clear")');
    await expect(clearBtn).toBeEnabled();

    // Click Clear
    await clearBtn.click();

    // The empty state message should reappear
    await expect(page.locator('text=Send a message to start a conversation')).toBeVisible({
      timeout: 5000
    });

    // The messages should be gone
    await expect(page.locator('text=hello agent')).not.toBeVisible();
    await expect(page.locator('text=Hi! How can I help?')).not.toBeVisible();

    // Clear button should be disabled again
    await expect(clearBtn).toBeDisabled();
  });

  test('User messages show HTML and code verbatim', async ({ page, beans }) => {
    await agentSession('__central__', beans)
      .withMessages([
        { role: 'user', content: 'hello agent' },
        { role: 'assistant', content: 'Hi!' },
        {
          role: 'user',
          content:
            'Why does <b>this</b> fail? `a<b && c`\n\n```go\nif a < b && c > d {}\n```\n\n```\nx <= y & z\n```'
        },
        { role: 'assistant', content: 'Let me look.' }
      ])
      .open(page);

    const message = page.locator('.agent-prose', { hasText: 'Why does' });
    await expect(message).toContainText('Why does <b>this</b> fail?', { timeout: 5000 });
    // Typed HTML is text, not markup.
    await expect(message.locator('b')).toHaveCount(0);
    // Code is escaped once: no entities show up as text.
    await expect(message.locator('code', { hasText: 'a<b && c' })).toHaveCount(1);
    await expect(message.locator('pre', { hasText: 'if a < b && c > d {}' })).toHaveCount(1);
    await expect(message.locator('pre', { hasText: 'x <= y & z' })).toHaveCount(1);
    await expect(message).not.toContainText('&lt;');
    await expect(message).not.toContainText('&amp;');
  });

  test('Image attachments are displayed inline in user messages', async ({ page, beans }) => {
    // Create a tiny 1x1 red PNG for testing
    const pngData = Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==',
      'base64'
    );
    const imageId = 'test-image.png';

    // Write the image file to the attachments directory
    const attachDir = join(beans.beansPath, '.conversations', 'attachments', '__central__');
    mkdirSync(attachDir, { recursive: true });
    writeFileSync(join(attachDir, imageId), pngData);

    // Seed a conversation with an image reference
    await agentSession('__central__', beans)
      .withMessages([
        {
          role: 'user',
          content: 'Check this screenshot',
          images: [{ id: imageId, media_type: 'image/png' }]
        },
        { role: 'assistant', content: 'I can see the image.' }
      ])
      .open(page);

    // Verify the text message is visible
    await expect(page.locator('text=Check this screenshot')).toBeVisible({ timeout: 5000 });

    // Verify the image is rendered inline
    const img = page.locator('img[alt="Attachment"]');
    await expect(img).toBeVisible({ timeout: 5000 });

    // Verify the image src points to the attachment endpoint
    const src = await img.getAttribute('src');
    expect(src).toContain('/api/attachments/__central__/test-image.png');
  });
});
