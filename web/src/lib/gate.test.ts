import { describe, expect, it } from 'vitest';
import { fmtGate } from './gate';

describe('fmtGate', () => {
  it('combines terminal and gate', () => {
    expect(fmtGate('5', 'B32')).toBe('Terminal 5 · Gate B32');
  });
  it('gate only', () => {
    expect(fmtGate('', 'B32')).toBe('Gate B32');
  });
  it('terminal only', () => {
    expect(fmtGate('5', '')).toBe('Terminal 5');
  });
  it('neither → Unknown', () => {
    expect(fmtGate('', '')).toBe('Unknown');
    expect(fmtGate(undefined, undefined)).toBe('Unknown');
  });
});
