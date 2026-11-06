import { uniqueNamesGenerator, adjectives, animals } from 'unique-names-generator';

/** Words to exclude from generated workspace names. */
const blocklist = new Set([
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

/** Pre-filtered dictionaries with blocked words removed. */
export const safeAdjectives = adjectives.filter((w) => !blocklist.has(w));
const safeAnimals = animals.filter((w) => !blocklist.has(w));

function randomSuffix(length = 4): string {
  const chars = 'abcdefghijklmnopqrstuvwxyz0123456789';
  let result = '';
  for (let i = 0; i < length; i++) {
    result += chars[Math.floor(Math.random() * chars.length)];
  }
  return result;
}

export function generateWorkspaceName(): string {
  const base = uniqueNamesGenerator({
    dictionaries: [safeAdjectives, safeAnimals],
    separator: '-',
    length: 2
  });
  return `${base}-${randomSuffix()}`;
}
