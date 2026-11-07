import { describe, expect, it } from 'vitest';
import {
  createBeanIdMatcher,
  linkifyBeanIds,
  renderMarkdown,
  updateBeanIdMatcher
} from '$lib/markdown';

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

describe('bean links', () => {
  const custom = 'beans-e2e-template-pd4Yf8-';
  const matcher = createBeanIdMatcher([
    'beans-s1m0',
    'proj-ab12',
    `${custom}x9k2`,
    // A prefix of another ID: the longest ID wins.
    'beans-abcd',
    'beans-abcd-efgh'
  ]);
  const links = (html: string) =>
    [...html.matchAll(/<a data-bean-id="([^"]+)" class="bean-link">([^<]*)<\/a>/g)].map((m) => {
      expect(m[2]).toBe(m[1]);
      return m[1];
    });
  const render = (md: string) => renderMarkdown(md, { beanIds: matcher });

  it('links known IDs with the default prefix', async () => {
    expect(links(await render('See beans-s1m0 for details.'))).toEqual(['beans-s1m0']);
  });

  it('links known IDs with custom prefixes, also with dashes', async () => {
    expect(links(await render(`Blocked by proj-ab12 and ${custom}x9k2.`))).toEqual([
      'proj-ab12',
      `${custom}x9k2`
    ]);
  });

  it('does not link unknown IDs', async () => {
    expect(links(await render('beans-zzzz and proj-zz99 and beans-s1m'))).toEqual([]);
  });

  it('does not link IDs in code spans, code blocks or link texts', async () => {
    const html = await render(
      'Use `beans-s1m0` here.\n\n```\nbeans show beans-s1m0\n```\n\n[beans-s1m0](https://example.com)'
    );
    expect(links(html)).toEqual([]);
    expect(html).toContain('<code>beans-s1m0</code>');
  });

  it('respects word boundaries', async () => {
    const md = [
      'xbeans-s1m0',
      'beans-s1m0x',
      'beans-s1m0_x',
      'foo-beans-s1m0',
      'beans-s1m0-ish',
      '(beans-s1m0)',
      'beans-s1m0, beans-s1m0.',
      '**beans-s1m0**',
      'beans-abcd-efgh',
      'beans-abcd-efg'
    ].join('\n\n');
    expect(links(await render(md))).toEqual([
      'beans-s1m0', // beans-s1m0-ish
      'beans-s1m0',
      'beans-s1m0',
      'beans-s1m0',
      'beans-s1m0',
      'beans-abcd-efgh',
      'beans-abcd'
    ]);
  });

  it('links nothing without a matcher', async () => {
    expect(links(await renderMarkdown('See beans-s1m0.'))).toEqual([]);
  });

  it('linkifyBeanIds links known IDs in escaped plain text', () => {
    expect(linkifyBeanIds(`<b> beans-s1m0 & ${custom}x9k2, beans-zzzz`, matcher)).toBe(
      '&lt;b&gt; <a data-bean-id="beans-s1m0" class="bean-link">beans-s1m0</a> &amp; ' +
        `<a data-bean-id="${custom}x9k2" class="bean-link">${custom}x9k2</a>, beans-zzzz`
    );
    expect(linkifyBeanIds('nothing beans-zzzz xbeans-s1m0', matcher)).toBeUndefined();
  });

  it('updateBeanIdMatcher keeps the matcher while the IDs are the same', () => {
    const a = updateBeanIdMatcher(undefined, ['beans-s1m0', 'proj-ab12']);
    expect(updateBeanIdMatcher(a, ['proj-ab12', 'beans-s1m0'])).toBe(a);
    const b = updateBeanIdMatcher(a, ['proj-ab12']);
    expect(b).not.toBe(a);
    expect(b.find('beans-s1m0 proj-ab12')).toEqual({ index: 11, id: 'proj-ab12' });
  });
});
