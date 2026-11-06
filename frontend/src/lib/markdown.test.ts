import { describe, expect, it } from 'vitest';
import { renderMarkdown } from '$lib/markdown';

describe('renderMarkdown with escapeRawHtml (user messages)', () => {
  it('shows HTML tags as text', async () => {
    const html = await renderMarkdown('hello <b>world</b> <script>alert(1)</script>', {
      escapeRawHtml: true
    });
    expect(html).toContain('&lt;b&gt;world&lt;/b&gt;');
    expect(html).toContain('&lt;script&gt;');
    expect(html).not.toContain('<b>');
    expect(html).not.toContain('<script>');
  });

  it('shows HTML blocks as text', async () => {
    const html = await renderMarkdown('<div class="x">\nhi\n</div>', { escapeRawHtml: true });
    expect(html).toContain('&lt;div class=&quot;x&quot;&gt;');
    expect(html).not.toContain('<div');
  });

  it('escapes code blocks only once', async () => {
    const html = await renderMarkdown('```\nif (a < b && c > d) {}\n```', { escapeRawHtml: true });
    expect(html).toContain('if (a &lt; b &amp;&amp; c &gt; d) {}');
    expect(html).not.toContain('&amp;lt;');
    expect(html).not.toContain('&amp;amp;');
  });

  it('escapes inline code only once', async () => {
    const html = await renderMarkdown('use `a<b && "c"` here', { escapeRawHtml: true });
    expect(html).toMatch(/<code>a&lt;b &amp;&amp; (&quot;|")c(&quot;|")<\/code>/);
    expect(html).not.toContain('&amp;lt;');
  });

  it('keeps plain text with angle brackets and ampersands readable', async () => {
    const html = await renderMarkdown('a<b & c>d', { escapeRawHtml: true });
    expect(html).toContain('a&lt;b &amp; c&gt;d');
    expect(html).not.toContain('&amp;lt;');
  });

  it('still renders markdown', async () => {
    const html = await renderMarkdown('**bold** and [link](https://example.com)', {
      escapeRawHtml: true
    });
    expect(html).toContain('<strong>bold</strong>');
    expect(html).toContain('href="https://example.com"');
  });
});

describe('renderMarkdown without escapeRawHtml', () => {
  it('passes raw HTML through (sanitized in the browser)', async () => {
    const html = await renderMarkdown('hello <b>world</b>');
    expect(html).toContain('<b>world</b>');
  });
});
