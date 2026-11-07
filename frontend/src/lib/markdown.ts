import { Marked, type MarkedExtension } from 'marked';
import DOMPurify from 'dompurify';
import { browser } from '$app/environment';
import { createHighlighterCore, type HighlighterCore } from 'shiki/core';
import { createOnigurumaEngine } from 'shiki/engine/oniguruma';
import githubDark from 'shiki/themes/github-dark.mjs';
import githubLight from 'shiki/themes/github-light.mjs';

// Import only essential languages to keep bundle small
import langJavascript from 'shiki/langs/javascript.mjs';
import langTypescript from 'shiki/langs/typescript.mjs';
import langGo from 'shiki/langs/go.mjs';
import langBash from 'shiki/langs/bash.mjs';
import langJson from 'shiki/langs/json.mjs';
import langYaml from 'shiki/langs/yaml.mjs';
import langMarkdown from 'shiki/langs/markdown.mjs';
import langGraphql from 'shiki/langs/graphql.mjs';
import langDiff from 'shiki/langs/diff.mjs';
import langRuby from 'shiki/langs/ruby.mjs';
import langPython from 'shiki/langs/python.mjs';
import langRust from 'shiki/langs/rust.mjs';
import langSql from 'shiki/langs/sql.mjs';
import langHtml from 'shiki/langs/html.mjs';
import langCss from 'shiki/langs/css.mjs';
import langSvelte from 'shiki/langs/svelte.mjs';
import langJsx from 'shiki/langs/jsx.mjs';
import langTsx from 'shiki/langs/tsx.mjs';
import langToml from 'shiki/langs/toml.mjs';
import langDockerfile from 'shiki/langs/dockerfile.mjs';
import langC from 'shiki/langs/c.mjs';
import langCpp from 'shiki/langs/cpp.mjs';
import langJava from 'shiki/langs/java.mjs';
import langPhp from 'shiki/langs/php.mjs';
import langSwift from 'shiki/langs/swift.mjs';
import langKotlin from 'shiki/langs/kotlin.mjs';
import langCsharp from 'shiki/langs/csharp.mjs';
import langLua from 'shiki/langs/lua.mjs';
import langElixir from 'shiki/langs/elixir.mjs';

const BUNDLED_LANGS = [
  langJavascript,
  langTypescript,
  langJsx,
  langTsx,
  langGo,
  langBash,
  langJson,
  langYaml,
  langMarkdown,
  langGraphql,
  langDiff,
  langRuby,
  langPython,
  langRust,
  langSql,
  langHtml,
  langCss,
  langSvelte,
  langToml,
  langDockerfile,
  langC,
  langCpp,
  langJava,
  langPhp,
  langSwift,
  langKotlin,
  langCsharp,
  langLua,
  langElixir
];

// Languages we have bundled (including aliases)
const SUPPORTED_LANGS = new Set([
  'javascript',
  'js',
  'typescript',
  'ts',
  'go',
  'bash',
  'sh',
  'shell',
  'zsh',
  'json',
  'yaml',
  'yml',
  'markdown',
  'md',
  'graphql',
  'gql',
  'diff',
  'ruby',
  'rb',
  'python',
  'py',
  'rust',
  'rs',
  'sql',
  'html',
  'css',
  'svelte',
  'jsx',
  'tsx',
  'toml',
  'dockerfile',
  'docker',
  'c',
  'cpp',
  'c++',
  'java',
  'php',
  'swift',
  'kotlin',
  'kt',
  'csharp',
  'c#',
  'cs',
  'lua',
  'elixir',
  'ex'
]);

// Common language aliases
const LANG_ALIASES: Record<string, string> = {
  js: 'javascript',
  ts: 'typescript',
  sh: 'bash',
  zsh: 'bash',
  shell: 'bash',
  yml: 'yaml',
  md: 'markdown',
  gql: 'graphql',
  rb: 'ruby',
  py: 'python',
  rs: 'rust',
  docker: 'dockerfile',
  'c++': 'cpp',
  kt: 'kotlin',
  'c#': 'csharp',
  cs: 'csharp',
  ex: 'elixir'
};

let highlighter: HighlighterCore | null = null;
let highlighterPromise: Promise<HighlighterCore> | null = null;

/**
 * Initialize the shiki highlighter with bundled languages only.
 * Only works in browser - returns null during SSR.
 */
async function getHighlighter(): Promise<HighlighterCore | null> {
  if (!browser) return null;

  if (highlighter) return highlighter;
  if (highlighterPromise) return highlighterPromise;

  highlighterPromise = createHighlighterCore({
    engine: createOnigurumaEngine(import('shiki/wasm')),
    themes: [githubDark, githubLight],
    langs: BUNDLED_LANGS
  });

  highlighter = await highlighterPromise;
  return highlighter;
}

/**
 * Custom marked extension for shiki syntax highlighting
 */
function shikiExtension(hl: HighlighterCore): MarkedExtension {
  return {
    renderer: {
      code({ text, lang }) {
        const rawLang = (lang || '').toLowerCase();
        const language = LANG_ALIASES[rawLang] || rawLang;

        // Only highlight supported languages
        if (SUPPORTED_LANGS.has(rawLang) || SUPPORTED_LANGS.has(language)) {
          try {
            return hl.codeToHtml(text, {
              lang: language,
              themes: {
                light: 'github-light',
                dark: 'github-dark'
              },
              defaultColor: false,
              cssVariablePrefix: '--shiki-'
            });
          } catch {
            // Fall through to plain rendering
          }
        }

        // Fallback for unsupported languages
        const escaped = text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        return `<pre class="shiki"><code>${escaped}</code></pre>`;
      }
    }
  };
}

/**
 * Finds bean IDs in text. Bean IDs are the configured prefix plus a random
 * suffix (the prefix may contain dashes, e.g. `beans-e2e-template-pd4Yf8-`),
 * so only the IDs of existing beans are matched, see createBeanIdMatcher.
 */
export interface BeanIdMatcher {
  /** The known bean IDs. */
  readonly ids: ReadonlySet<string>;
  /** The first bean ID in text at or after `from`, if any. */
  find(text: string, from?: number): { index: number; id: string } | undefined;
  /** The bean ID at the very start of text, if any. */
  matchStart(text: string): string | undefined;
}

// A bean ID candidate: a run of ID characters. A bean ID is a prefix of a
// candidate ending at its end or before a '-' or '.' ("beans-abcd." ends a
// sentence, "beans-abcd-ish" is a compound). It must not follow an ID
// character (word boundary).
const ID_CHAR = /[A-Za-z0-9_.-]/;
const CANDIDATE = /(?<![A-Za-z0-9_.-])[A-Za-z0-9_][A-Za-z0-9_.-]*/g;
const CANDIDATE_START = /^[A-Za-z0-9_][A-Za-z0-9_.-]*/;

/**
 * Create a matcher for the given bean IDs (e.g. the beans of the beans store).
 */
export function createBeanIdMatcher(ids: Iterable<string>): BeanIdMatcher {
  const known = new Set(ids);
  // The longest known ID that the candidate starts with.
  const longestId = (candidate: string): string | undefined => {
    for (let end = candidate.length; end > 0; end--) {
      if (end < candidate.length && candidate[end] !== '-' && candidate[end] !== '.') continue;
      const id = candidate.slice(0, end);
      if (known.has(id)) return id;
    }
    return undefined;
  };
  return {
    ids: known,
    find(text, from = 0) {
      if (known.size === 0) return undefined;
      const pattern = new RegExp(CANDIDATE.source, 'g');
      pattern.lastIndex = from;
      for (let m = pattern.exec(text); m; m = pattern.exec(text)) {
        const id = longestId(m[0]);
        if (id) return { index: m.index, id };
      }
      return undefined;
    },
    matchStart(text) {
      if (known.size === 0) return undefined;
      const m = text.match(CANDIDATE_START);
      return m ? longestId(m[0]) : undefined;
    }
  };
}

/**
 * Return prev if it matches exactly the given IDs, else a new matcher. Use it
 * to keep a matcher stable while beans change but no bean is added or removed,
 * so rendered markdown is not re-rendered needlessly.
 */
export function updateBeanIdMatcher(
  prev: BeanIdMatcher | undefined,
  ids: Iterable<string>
): BeanIdMatcher {
  const next = new Set(ids);
  if (prev && prev.ids.size === next.size && [...next].every((id) => prev.ids.has(id))) {
    return prev;
  }
  return createBeanIdMatcher(next);
}

function beanLinkHtml(id: string): string {
  const escaped = escapeHtml(id);
  return `<a data-bean-id="${escaped}" class="bean-link">${escaped}</a>`;
}

/**
 * Marked extension that auto-links the bean IDs found by matcher in inline
 * text (not in code or link texts). Renders as <a data-bean-id="..."> so
 * click handlers can navigate.
 */
export function beanLinkExtension(matcher: BeanIdMatcher): MarkedExtension {
  return {
    extensions: [
      {
        name: 'beanLink',
        level: 'inline',
        start(src: string) {
          return matcher.find(src)?.index;
        },
        tokenizer(src, tokens) {
          if (this.lexer.state.inLink) return undefined;
          // Marked offers every position; require a word boundary before.
          const prev = tokens.at(-1)?.raw.slice(-1);
          if (prev && ID_CHAR.test(prev)) return undefined;
          const beanId = matcher.matchStart(src);
          if (!beanId) return undefined;
          return { type: 'beanLink', raw: beanId, beanId };
        },
        renderer(token) {
          return beanLinkHtml((token as Record<string, unknown>).beanId as string);
        }
      }
    ]
  };
}

export interface RenderMarkdownOptions {
  /**
   * Show raw HTML in the source (tags, HTML blocks) as text instead of
   * rendering it, e.g. for messages typed by the user. Code is escaped by the
   * code renderers, so the source must not be escaped beforehand (that would
   * escape code twice).
   */
  escapeRawHtml?: boolean;
  /** Link the bean IDs found by this matcher (default: no bean links). */
  beanIds?: BeanIdMatcher;
}

/**
 * Marked extension rendering raw HTML tokens as escaped text.
 */
function escapeRawHtmlExtension(): MarkedExtension {
  return {
    renderer: {
      html({ text }) {
        return escapeHtml(text);
      }
    }
  };
}

/**
 * Render markdown to HTML with syntax highlighting.
 * Falls back to plain code blocks during SSR.
 */
export async function renderMarkdown(
  content: string,
  options: RenderMarkdownOptions = {}
): Promise<string> {
  if (!content) return '';

  const md = new Marked();
  md.use({ gfm: true, breaks: true });
  if (options.escapeRawHtml) {
    md.use(escapeRawHtmlExtension());
  }
  if (options.beanIds) {
    md.use(beanLinkExtension(options.beanIds));
  }
  md.use({
    renderer: {
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens);
        const titleAttr = title ? ` title="${title}"` : '';
        return `<a href="${href}"${titleAttr} target="_blank" rel="noopener noreferrer">${text}</a>`;
      }
    }
  });

  const hl = await getHighlighter();
  if (hl) {
    md.use(shikiExtension(hl));
  } else {
    md.use(plainCodeExtension());
  }

  const html = md.parse(content) as string;

  if (browser) {
    return DOMPurify.sanitize(html, {
      ADD_TAGS: ['input'],
      ADD_ATTR: ['data-bean-id', 'target', 'rel', 'checked', 'disabled', 'type']
    });
  }

  return html;
}

/**
 * Plain code block extension for SSR (no syntax highlighting)
 */
function plainCodeExtension(): MarkedExtension {
  return {
    renderer: {
      code({ text }) {
        const escaped = text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        return `<pre class="shiki"><code>${escaped}</code></pre>`;
      }
    }
  };
}

/**
 * Escape HTML special characters in plain text.
 */
export function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

/**
 * Replace the bean IDs found by matcher in plain text with clickable
 * bean-link HTML. The text is HTML-escaped, so the result is safe for
 * innerHTML. Returns undefined if no bean IDs are found (caller can skip
 * innerHTML).
 */
export function linkifyBeanIds(text: string, matcher: BeanIdMatcher): string | undefined {
  let out = '';
  let last = 0;
  for (let m = matcher.find(text); m; m = matcher.find(text, last)) {
    out += escapeHtml(text.slice(last, m.index)) + beanLinkHtml(m.id);
    last = m.index + m.id.length;
  }
  if (last === 0) return undefined;
  return out + escapeHtml(text.slice(last));
}

/**
 * Pre-initialize the highlighter (call on app start for faster first render)
 */
export function preloadHighlighter(): void {
  getHighlighter().catch(console.error);
}
