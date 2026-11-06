import { describe, it, expect } from 'vitest';
import { generateWorkspaceName, safeAdjectives } from './nameGenerator';

describe('safeAdjectives', () => {
  const blocked = [
    'aggressive',
    'angry',
    'bloody',
    'crude',
    'cruel',
    'dead',
    'dirty',
    'drunk',
    'evil',
    'fat',
    'fatal',
    'hostile',
    'lazy',
    'naked',
    'naughty',
    'sexual',
    'stupid',
    'toxic',
    'ugly',
    'vicious',
    'violent',
    'wicked'
  ];

  it('does not contain any blocked words', () => {
    expect.assertions(1);
    const found = safeAdjectives.filter((w) => blocked.includes(w));
    expect(found).toEqual([]);
  });

  it('still has a reasonable number of adjectives', () => {
    expect.assertions(1);
    expect(safeAdjectives.length).toBeGreaterThan(1000);
  });
});

describe('generateWorkspaceName', () => {
  it('returns a name with adjective-animal-suffix format', () => {
    expect.assertions(2);
    const name = generateWorkspaceName();
    const parts = name.split('-');
    expect(parts.length).toBeGreaterThanOrEqual(3);
    expect(parts[parts.length - 1]).toMatch(/^[a-z0-9]{4}$/);
  });

  it('never produces blocked words across many generations', () => {
    expect.assertions(1);
    const blocked = new Set([
      'aggressive',
      'angry',
      'bloody',
      'crude',
      'cruel',
      'dead',
      'dirty',
      'drunk',
      'evil',
      'fat',
      'fatal',
      'hostile',
      'lazy',
      'naked',
      'naughty',
      'sexual',
      'stupid',
      'toxic',
      'ugly',
      'vicious',
      'violent',
      'wicked'
    ]);
    const names = Array.from({ length: 500 }, () => generateWorkspaceName());
    const violations = names.filter((name) => name.split('-').some((word) => blocked.has(word)));
    expect(violations).toEqual([]);
  });
});
