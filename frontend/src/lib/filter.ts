/**
 * Structured bean filter for the planning views (backlog + board).
 *
 * A filter is a plain query string — the single source of truth that is shown
 * in the filter input, persisted in the URL (`?q=`) and could be stored as a
 * "saved filter" later. This module parses it into terms, formats terms back
 * into a query string, edits it (for the filter popover and chips), matches
 * beans against it and produces autocomplete suggestions. It is pure (no
 * Svelte state, no I/O) so it can be unit tested in isolation.
 *
 * Syntax (terms are ANDed; comma-separated values within a term are ORed):
 *
 *   status:todo,draft      bean status is one of the values
 *   type:bug,feature       bean type is one of the values
 *   priority:high,critical priority is one of the values (empty = normal)
 *   priority:>=high        priority comparison (>=, <=, >, <); higher = more urgent
 *   tag:ui,backend         bean has at least one of the tags
 *   parent:<id|title>      bean is a descendant (any depth) of the referenced bean
 *   milestone:<id|title>   like parent:, but the ancestor must be a milestone
 *   epic:<id|title>        like parent:, but the ancestor must be an epic
 *   blocked:yes|no         bean is (not) blocked, directly or via an ancestor
 *   ready:yes|no           bean is (not) ready to start, like `beans list --ready`:
 *                          not blocked, not draft/in-progress/completed/scrapped
 *                          and no completed/scrapped ancestor
 *   done:yes|no            bean is (not) completed/scrapped, itself or via an ancestor
 *   archived:yes|no        bean is (not) stored in the archive directory (the
 *                          planning views hide archived beans unless the query
 *                          has an archived: clause, see mentionsArchived)
 *   -status:completed      a leading "-" negates a field term
 *   "some words"           quotes group values/text containing spaces
 *   anything else          free-text search on title, id, type, status and tags
 *
 * The same syntax is implemented in Go by pkg/beangraph/filterquery (terminal
 * UI, `beans list --query`); keep both in sync.
 */
import type { Bean } from '$lib/beans.svelte';

/** URL query param holding the planning filter query (`/planning?q=status:todo`). */
export const FILTER_URL_PARAM = 'q';

export const STATUSES = ['draft', 'todo', 'in-progress', 'completed', 'scrapped'] as const;
export const TYPES = ['milestone', 'epic', 'feature', 'bug', 'task'] as const;
/** Ordered from most to least urgent. */
export const PRIORITIES = ['critical', 'high', 'normal', 'low', 'deferred'] as const;
const DEFAULT_PRIORITY = 'normal';
const RESOLVED_STATUSES = new Set(['completed', 'scrapped']);
const NOT_READY_STATUSES = new Set(['draft', 'in-progress', 'completed', 'scrapped']);

export type FilterField =
  | 'status'
  | 'type'
  | 'priority'
  | 'tag'
  | 'parent'
  | 'milestone'
  | 'epic'
  | 'blocked'
  | 'ready'
  | 'done'
  | 'archived';

export const FILTER_FIELDS: readonly FilterField[] = [
  'status',
  'type',
  'priority',
  'tag',
  'parent',
  'milestone',
  'epic',
  'blocked',
  'ready',
  'done',
  'archived'
];

/** Alternative spellings accepted by the parser (normalized on parse). */
const FIELD_ALIASES: Record<string, FilterField> = {
  tags: 'tag',
  prio: 'priority',
  under: 'parent'
};

/** Short help shown in autocomplete for each field. */
export const FIELD_HELP: Record<FilterField, string> = {
  status: 'Status is one of…',
  type: 'Type is one of…',
  priority: 'Priority is one of…, or >=, <= a level',
  tag: 'Has one of the tags…',
  parent: 'Below a bean (any depth)',
  milestone: 'Below a milestone',
  epic: 'Below an epic',
  blocked: 'Blocked: yes or no',
  ready: 'Ready to start: yes or no',
  done: 'Completed/scrapped: yes or no',
  archived: 'Archived: yes or no'
};

/** Fields whose values reference other beans. */
export const PARENT_FIELDS: readonly FilterField[] = ['parent', 'milestone', 'epic'];

/** Fields that take yes/no values. */
export const BOOL_FIELDS: readonly FilterField[] = ['blocked', 'ready', 'done', 'archived'];

export type CompareOp = '=' | '>=' | '<=' | '>' | '<';

export interface FilterClause {
  kind: 'clause';
  field: FilterField;
  negate: boolean;
  /** Comparison operator; only priority supports anything other than '='. */
  op: CompareOp;
  /** Alternatives (ORed). Normalized to lower case except for parent refs. */
  values: string[];
}

export interface TextTerm {
  kind: 'text';
  value: string;
}

export type FilterTerm = FilterClause | TextTerm;

export interface FilterQuery {
  terms: FilterTerm[];
}

// ---------------------------------------------------------------------------
// Tokenizing / parsing / formatting
// ---------------------------------------------------------------------------

export interface Token {
  /** Raw token text, including quotes. */
  raw: string;
  start: number;
  end: number;
}

/** Split a query on whitespace, keeping double-quoted sections together. */
export function tokenize(query: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;
  while (i < query.length) {
    while (i < query.length && /\s/.test(query[i])) i++;
    if (i >= query.length) break;
    const start = i;
    let inQuote = false;
    while (i < query.length && (inQuote || !/\s/.test(query[i]))) {
      if (query[i] === '"') inQuote = !inQuote;
      i++;
    }
    tokens.push({ raw: query.slice(start, i), start, end: i });
  }
  return tokens;
}

function unquote(s: string): string {
  return s.replace(/"/g, '');
}

/** Split on commas that are not inside double quotes. */
function splitValues(s: string): string[] {
  const parts: string[] = [];
  let current = '';
  let inQuote = false;
  for (const ch of s) {
    if (ch === '"') inQuote = !inQuote;
    if (ch === ',' && !inQuote) {
      parts.push(current);
      current = '';
    } else {
      current += ch;
    }
  }
  parts.push(current);
  return parts;
}

function resolveField(name: string): FilterField | null {
  const lower = name.toLowerCase();
  if ((FILTER_FIELDS as readonly string[]).includes(lower)) return lower as FilterField;
  return FIELD_ALIASES[lower] ?? null;
}

const FIELD_TOKEN = /^(-?)([a-zA-Z]+):(.*)$/s;

/** Parse a single token into a term, or null for an empty token. */
export function parseToken(raw: string): FilterTerm | null {
  const m = raw.match(FIELD_TOKEN);
  const field = m ? resolveField(m[2]) : null;
  if (m && field) {
    let rest = m[3];
    let op: CompareOp = '=';
    if (field === 'priority') {
      const opMatch = rest.match(/^(>=|<=|>|<|=)/);
      if (opMatch) {
        op = opMatch[1] as CompareOp;
        rest = rest.slice(opMatch[1].length);
      }
    }
    const keepCase = PARENT_FIELDS.includes(field);
    const values = splitValues(rest)
      .map((v) => unquote(v).trim())
      .filter(Boolean)
      .map((v) => (keepCase ? v : v.toLowerCase()));
    return { kind: 'clause', field, negate: m[1] === '-', op, values };
  }
  const value = unquote(raw).trim();
  return value ? { kind: 'text', value } : null;
}

export function parseFilter(query: string): FilterQuery {
  const terms: FilterTerm[] = [];
  for (const token of tokenize(query)) {
    const term = parseToken(token.raw);
    if (term) terms.push(term);
  }
  return { terms };
}

function quoteIfNeeded(value: string): string {
  return /[\s,"]/.test(value) ? `"${value.replace(/"/g, '')}"` : value;
}

export function formatTerm(term: FilterTerm): string {
  if (term.kind === 'text') return quoteIfNeeded(term.value);
  const op = term.op === '=' ? '' : term.op;
  return `${term.negate ? '-' : ''}${term.field}:${op}${term.values.map(quoteIfNeeded).join(',')}`;
}

export function formatFilter(query: FilterQuery): string {
  return query.terms.map(formatTerm).join(' ');
}

/** Clauses that actually constrain results (incomplete `status:` terms do not). */
export function activeClauses(query: FilterQuery): FilterClause[] {
  return query.terms.filter((t): t is FilterClause => t.kind === 'clause' && t.values.length > 0);
}

/**
 * Whether the query has an archived: clause. The planning views hide archived
 * beans by default; such a clause (archived:yes, -archived:no, archived:yes,no,
 * …) makes them candidates, so the clause alone decides about them.
 */
export function mentionsArchived(query: FilterQuery): boolean {
  return activeClauses(query).some((c) => c.field === 'archived');
}

export function isEmptyFilter(query: FilterQuery): boolean {
  return query.terms.every((t) => t.kind === 'clause' && t.values.length === 0);
}

/** Values the parser accepts for a field, or null when any value is allowed. */
export function knownValues(field: FilterField): readonly string[] | null {
  switch (field) {
    case 'status':
      return STATUSES;
    case 'type':
      return TYPES;
    case 'priority':
      return PRIORITIES;
    default:
      return BOOL_FIELDS.includes(field) ? ['yes', 'no'] : null;
  }
}

const BOOLEAN_VALUES: Record<string, boolean> = {
  yes: true,
  true: true,
  y: true,
  '1': true,
  no: false,
  false: false,
  n: false,
  '0': false
};

/** Values of a clause that cannot match anything (typos etc.), for UI hints. */
export function invalidValues(clause: FilterClause): string[] {
  if (BOOL_FIELDS.includes(clause.field)) {
    return clause.values.filter((v) => !(v in BOOLEAN_VALUES));
  }
  if (clause.op !== '=' && clause.field !== 'priority') return clause.values;
  const known = knownValues(clause.field);
  return known ? clause.values.filter((v) => !known.includes(v)) : [];
}

// ---------------------------------------------------------------------------
// Editing (used by the popover and chips; each returns a new query)
// ---------------------------------------------------------------------------

function isPlainClause(term: FilterTerm, field: FilterField): term is FilterClause {
  return term.kind === 'clause' && term.field === field && !term.negate && term.op === '=';
}

/** Values of the first positive `field:a,b` clause (what the popover shows as checked). */
export function selectedValues(query: FilterQuery, field: FilterField): string[] {
  const clause = query.terms.find((t) => isPlainClause(t, field)) as FilterClause | undefined;
  return clause?.values ?? [];
}

/** Toggle one value in the first positive clause for `field`, creating/removing it as needed. */
export function toggleValue(query: FilterQuery, field: FilterField, value: string): FilterQuery {
  const index = query.terms.findIndex((t) => isPlainClause(t, field));
  if (index < 0) {
    return {
      terms: [...query.terms, { kind: 'clause', field, negate: false, op: '=', values: [value] }]
    };
  }
  const clause = query.terms[index] as FilterClause;
  const values = clause.values.includes(value)
    ? clause.values.filter((v) => v !== value)
    : [...clause.values, value];
  const terms = [...query.terms];
  if (values.length === 0) terms.splice(index, 1);
  else terms[index] = { ...clause, values };
  return { terms };
}

/** Replace every clause for `field` with a single positive clause (or none if `values` is empty). */
export function setClause(query: FilterQuery, field: FilterField, values: string[]): FilterQuery {
  const index = query.terms.findIndex((t) => t.kind === 'clause' && t.field === field);
  const terms = query.terms.filter((t) => !(t.kind === 'clause' && t.field === field));
  if (values.length > 0) {
    const clause: FilterClause = { kind: 'clause', field, negate: false, op: '=', values };
    terms.splice(index < 0 ? terms.length : index, 0, clause);
  }
  return { terms };
}

export function removeTerm(query: FilterQuery, index: number): FilterQuery {
  return { terms: query.terms.filter((_, i) => i !== index) };
}

/** Drop all field clauses, keeping only the free-text terms. */
export function clearClauses(query: FilterQuery): FilterQuery {
  return { terms: query.terms.filter((t) => t.kind === 'text') };
}

/** blocked:yes → true, blocked:no → false, otherwise null. */
export function blockedSelection(query: FilterQuery): boolean | null {
  const clause = query.terms.find((t) => isPlainClause(t, 'blocked')) as FilterClause | undefined;
  const v = clause?.values[0];
  return v !== undefined && v in BOOLEAN_VALUES ? BOOLEAN_VALUES[v] : null;
}

export function setBlocked(query: FilterQuery, blocked: boolean | null): FilterQuery {
  return setClause(query, 'blocked', blocked === null ? [] : [blocked ? 'yes' : 'no']);
}

// ---------------------------------------------------------------------------
// Matching
// ---------------------------------------------------------------------------

/** Lookups needed to evaluate relational clauses (parent, blocked, ready, done). */
export interface FilterContext {
  /** All known beans (including archived ones) by ID. */
  byId: ReadonlyMap<string, Bean>;
  /** For each bean ID, the beans that list it in their `blockingIds`. */
  incomingBlockers: ReadonlyMap<string, Bean[]>;
}

export function createFilterContext(beans: Iterable<Bean>): FilterContext {
  const byId = new Map<string, Bean>();
  const incomingBlockers = new Map<string, Bean[]>();
  for (const bean of beans) {
    byId.set(bean.id, bean);
    for (const blockedId of bean.blockingIds ?? []) {
      const list = incomingBlockers.get(blockedId);
      if (list) list.push(bean);
      else incomingBlockers.set(blockedId, [bean]);
    }
  }
  return { byId, incomingBlockers };
}

/** Walk the parent chain starting at (and excluding) `bean`, guarding against cycles. */
function* ancestors(bean: Bean, byId: ReadonlyMap<string, Bean>): Generator<Bean> {
  const seen = new Set([bean.id]);
  let parentId = bean.parentId;
  while (parentId && !seen.has(parentId)) {
    const parent = byId.get(parentId);
    if (!parent) return;
    yield parent;
    seen.add(parentId);
    parentId = parent.parentId;
  }
}

function shortId(id: string): string {
  const dash = id.indexOf('-');
  return dash >= 0 ? id.slice(dash + 1) : id;
}

/** Whether `ref` (an ID, short ID, slug or title, case-insensitive) identifies `bean`. */
export function refersTo(bean: Bean, ref: string): boolean {
  const r = ref.toLowerCase();
  return (
    bean.id.toLowerCase() === r ||
    shortId(bean.id).toLowerCase() === r ||
    (!!bean.slug && bean.slug.toLowerCase() === r) ||
    bean.title.toLowerCase() === r
  );
}

/** Whether the bean is stored in the archive directory (mirrors bean.IsArchived). */
export function isArchived(bean: Pick<Bean, 'path'>): boolean {
  return bean.path.startsWith('archive/') || bean.path.startsWith('archive\\');
}

/** Case-insensitive substring match of one free-text term (the original filter behavior). */
export function matchesText(bean: Bean, text: string): boolean {
  if (!text) return true;
  const lower = text.toLowerCase();
  return (
    bean.title.toLowerCase().includes(lower) ||
    bean.type.toLowerCase().includes(lower) ||
    bean.status.toLowerCase().includes(lower) ||
    bean.id.toLowerCase().includes(lower) ||
    bean.tags.some((tag) => tag.toLowerCase().includes(lower))
  );
}

function priorityRank(priority: string): number {
  const idx = (PRIORITIES as readonly string[]).indexOf(priority || DEFAULT_PRIORITY);
  return idx >= 0 ? idx : PRIORITIES.length;
}

function matchesPriority(bean: Bean, clause: FilterClause): boolean {
  const priority = bean.priority || DEFAULT_PRIORITY;
  if (clause.op === '=') return clause.values.includes(priority);
  const rank = priorityRank(priority);
  return clause.values.some((v) => {
    const target = (PRIORITIES as readonly string[]).indexOf(v);
    if (target < 0) return false;
    // Lower rank = more urgent, so ">= high" means rank <= rank(high).
    switch (clause.op) {
      case '>=':
        return rank <= target;
      case '>':
        return rank < target;
      case '<=':
        return rank >= target;
      case '<':
        return rank > target;
      default:
        return false;
    }
  });
}

export type BeanPredicate = (bean: Bean) => boolean;

/**
 * Compile a filter into a predicate. Relational lookups (parent references,
 * blocked and done state) are resolved once per compiled filter and memoized.
 */
export function compileFilter(query: FilterQuery | string, ctx: FilterContext): BeanPredicate {
  const parsed = typeof query === 'string' ? parseFilter(query) : query;
  const clauses = activeClauses(parsed);
  const texts = parsed.terms.filter((t): t is TextTerm => t.kind === 'text').map((t) => t.value);
  if (clauses.length === 0 && texts.length === 0) return () => true;

  const blockedMemo = new Map<string, boolean>();
  const isActiveBlocker = (b: Bean | undefined) => !!b && !RESOLVED_STATUSES.has(b.status);
  const hasDirectBlockers = (bean: Bean) =>
    (bean.blockedByIds ?? []).some((id) => isActiveBlocker(ctx.byId.get(id))) ||
    (ctx.incomingBlockers.get(bean.id) ?? []).some(isActiveBlocker);
  // Mirrors beancore.IsBlocked: explicitly blocked, or an ancestor is.
  const isBlocked = (bean: Bean): boolean => {
    let result = blockedMemo.get(bean.id);
    if (result === undefined) {
      result = hasDirectBlockers(bean) || [...ancestors(bean, ctx.byId)].some(hasDirectBlockers);
      blockedMemo.set(bean.id, result);
    }
    return result;
  };

  const resolvedAncestorMemo = new Map<string, boolean>();
  // Mirrors beancore.ImplicitStatus != "": a completed/scrapped ancestor.
  const hasResolvedAncestor = (bean: Bean): boolean => {
    let result = resolvedAncestorMemo.get(bean.id);
    if (result === undefined) {
      result = [...ancestors(bean, ctx.byId)].some((a) => RESOLVED_STATUSES.has(a.status));
      resolvedAncestorMemo.set(bean.id, result);
    }
    return result;
  };
  // Mirrors `beans list --ready`: not blocked (explicitly or via an ancestor),
  // not draft/in-progress/completed/scrapped and no completed/scrapped ancestor.
  const isReady = (bean: Bean): boolean =>
    !NOT_READY_STATUSES.has(bean.status) && !hasResolvedAncestor(bean) && !isBlocked(bean);
  const isDone = (bean: Bean): boolean =>
    RESOLVED_STATUSES.has(bean.status) || hasResolvedAncestor(bean);
  const matchesBool = (clause: FilterClause, actual: boolean): boolean =>
    clause.values.some((v) => v in BOOLEAN_VALUES && BOOLEAN_VALUES[v] === actual);

  // Resolve parent references to the set of bean IDs they identify.
  const refTargets = new Map<FilterClause, Set<string>>();
  for (const clause of clauses) {
    if (!PARENT_FIELDS.includes(clause.field)) continue;
    const targets = new Set<string>();
    for (const bean of ctx.byId.values()) {
      if (clause.field !== 'parent' && bean.type !== clause.field) continue;
      if (clause.values.some((ref) => refersTo(bean, ref))) targets.add(bean.id);
    }
    refTargets.set(clause, targets);
  }

  const matchesClause = (bean: Bean, clause: FilterClause): boolean => {
    switch (clause.field) {
      case 'status':
        return clause.values.includes(bean.status);
      case 'type':
        return clause.values.includes(bean.type);
      case 'priority':
        return matchesPriority(bean, clause);
      case 'tag':
        return bean.tags.some((t) => clause.values.includes(t.toLowerCase()));
      case 'blocked':
        return matchesBool(clause, isBlocked(bean));
      case 'ready':
        return matchesBool(clause, isReady(bean));
      case 'done':
        return matchesBool(clause, isDone(bean));
      case 'archived':
        return matchesBool(clause, isArchived(bean));
      case 'parent':
      case 'milestone':
      case 'epic': {
        const targets = refTargets.get(clause)!;
        if (targets.size === 0) return false;
        for (const a of ancestors(bean, ctx.byId)) if (targets.has(a.id)) return true;
        return false;
      }
    }
  };

  return (bean) =>
    clauses.every((c) => matchesClause(bean, c) !== c.negate) &&
    texts.every((t) => matchesText(bean, t));
}

/** Result of filtering a bean hierarchy for tree views (backlog). */
export interface TreeFilterResult {
  /** Beans that match the filter themselves. */
  matched: Set<string>;
  /** Matched beans plus their ancestors (shown as context). */
  visible: Set<string>;
}

/** Compute matched beans and the ancestors needed to display them in a tree. */
export function filterTree(
  beans: Iterable<Bean>,
  predicate: BeanPredicate,
  byId: ReadonlyMap<string, Bean>
): TreeFilterResult {
  const matched = new Set<string>();
  const visible = new Set<string>();
  for (const bean of beans) {
    if (!predicate(bean)) continue;
    matched.add(bean.id);
    visible.add(bean.id);
    for (const a of ancestors(bean, byId)) {
      if (visible.has(a.id)) break;
      visible.add(a.id);
    }
  }
  return { matched, visible };
}

// ---------------------------------------------------------------------------
// Autocomplete
// ---------------------------------------------------------------------------

export interface Suggestion {
  /** Text shown in the list. */
  label: string;
  /** Secondary text (help, bean ID, …). */
  detail?: string;
  /** Replacement for the query range [start, end). */
  insert: string;
  start: number;
  end: number;
  /** Whether a further suggestion list is expected after accepting (e.g. after `status:`). */
  continues: boolean;
}

export interface SuggestionSources {
  tags: readonly string[];
  /** Beans that can be referenced by parent:/milestone:/epic:. */
  beans: readonly Pick<Bean, 'id' | 'title' | 'type'>[];
}

const MAX_SUGGESTIONS = 8;

/** Suggestions for the token at `caret` (only the part before the caret is considered). */
export function suggest(query: string, caret: number, sources: SuggestionSources): Suggestion[] {
  const token = tokenize(query).find((t) => t.start <= caret && caret <= t.end);
  if (!token) return [];
  const start = token.start;
  const prefix = query.slice(start, caret);
  const atTokenEnd = caret === token.end;

  // Field names: "sta" → "status:"
  const fieldMatch = prefix.match(/^(-?)([a-zA-Z]*)$/);
  if (fieldMatch && fieldMatch[2]) {
    const name = fieldMatch[2].toLowerCase();
    return FILTER_FIELDS.filter((f) => f.startsWith(name) && f !== name)
      .map((f) => ({
        label: `${fieldMatch[1]}${f}:`,
        detail: FIELD_HELP[f],
        insert: `${fieldMatch[1]}${f}:`,
        start,
        end: caret,
        continues: true
      }))
      .slice(0, MAX_SUGGESTIONS);
  }

  // Values: "status:todo,dr" → "draft"
  const m = prefix.match(FIELD_TOKEN);
  const field = m ? resolveField(m[2]) : null;
  if (!m || !field) return [];
  let valuePart = m[3];
  let valueStart = start + prefix.length - valuePart.length;
  if (field === 'priority') {
    const op = valuePart.match(/^(>=|<=|>|<|=)/);
    if (op) {
      valuePart = valuePart.slice(op[1].length);
      valueStart += op[1].length;
    }
  }
  const parts = splitValues(valuePart);
  const partial = parts[parts.length - 1];
  const chosen = new Set(parts.slice(0, -1).map((v) => unquote(v).trim().toLowerCase()));
  const partialStart = valueStart + valuePart.length - partial.length;
  const needle = unquote(partial).trim().toLowerCase();
  const suffix = atTokenEnd ? ' ' : '';

  const make = (value: string, label = value, detail?: string): Suggestion => ({
    label,
    detail,
    insert: quoteIfNeeded(value) + suffix,
    start: partialStart,
    end: caret,
    continues: false
  });

  let candidates: Suggestion[];
  if (PARENT_FIELDS.includes(field)) {
    candidates = sources.beans
      .filter((b) => field === 'parent' || b.type === field)
      .filter((b) => !chosen.has(b.id.toLowerCase()))
      .filter(
        (b) =>
          !needle || b.id.toLowerCase().includes(needle) || b.title.toLowerCase().includes(needle)
      )
      .map((b) => make(b.id, b.title, `${b.type} · ${b.id}`));
  } else {
    const values = knownValues(field) ?? [...sources.tags].sort();
    candidates = values
      .filter((v) => !chosen.has(v) && v.toLowerCase().startsWith(needle) && v !== needle)
      .map((v) => make(v));
  }
  return candidates.slice(0, MAX_SUGGESTIONS);
}
