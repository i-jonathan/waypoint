import { describe, expect, it } from 'vitest';
import { checklistLines, toggleItem } from '../src/lib/checklist';

// The client numbers items the way internal/domain/checklist.go does, character
// for character: a click on item n must tick the server's item n. Go's `.`
// matches everything but "\n", it strips every trailing "\r", and its
// TrimSpace knows Unicode spaces but not U+FEFF.

const lines = (body: string) => checklistLines(body).map((l) => l.line);

describe('items', () => {
  it('counts a line whose text holds U+2028, U+2029 or a lone CR', () => {
    expect(lines('- [ ] a b\n- [ ] c')).toEqual([0, 1]);
    expect(lines('- [ ] a b\n- [ ] c')).toEqual([0, 1]);
    expect(lines('- [ ] a\rb\n- [ ] c')).toEqual([0, 1]);
  });

  it('strips every trailing CR, not just one', () => {
    expect(lines('- [ ] a\r\r\n- [ ] b')).toEqual([0, 1]);
    expect(lines('- [ ]\r\r\n- [ ] b')).toEqual([0, 1]);
    expect(lines('- [ ]\r\r\r')).toEqual([0]);
  });

  it('ticks the item the server would, past such lines', () => {
    const body = '- [ ] a b\n- [ ] c\r\r\n- [ ] d\re';
    expect(toggleItem(body, 2)!.body).toBe('- [ ] a b\n- [x] c\r\r\n- [ ] d\re');
    expect(toggleItem(body, 3)!.body).toBe('- [ ] a b\n- [ ] c\r\r\n- [x] d\re');
    expect(toggleItem(body, 4)).toBeNull();
  });

  it('does not count `- [ ]x` or a mark that is not space, x or X', () => {
    expect(lines('- [ ]x\n- [y] a\n- [ ] ok')).toEqual([2]);
  });
});

describe('fences', () => {
  it('closes a fence only when what follows the run is Go whitespace', () => {
    // U+FEFF is not whitespace to Go, so this line does not close the fence.
    expect(lines('```\n- [ ] in\n```﻿\n- [ ] still in\n```\n- [ ] out')).toEqual([5]);
  });

  it('closes a fence followed by U+0085, U+00A0, U+2028 or U+3000, which Go trims', () => {
    for (const space of ['\u0085', ' ', ' ', ' ', '　', ' ', ' \t', '\r']) {
      expect(lines('```\n- [ ] in\n```' + space + '\n- [ ] out')).toEqual([3]);
    }
  });

  it('does not open a fence on a backtick run whose text holds a backtick, even past U+2028', () => {
    expect(lines('```a `\n- [ ] counted')).toEqual([1]);
  });

  it('strips every trailing CR before looking at a fence line', () => {
    expect(lines('```\r\r\n- [ ] in\n```\r\r\n- [ ] out')).toEqual([3]);
  });

  it('sees a fence whose info text holds a lone CR or U+2028', () => {
    expect(lines('~~~a\rb\n- [ ] in\n~~~\n- [ ] out')).toEqual([3]);
    expect(lines('~~~a b\n- [ ] in\n~~~\n- [ ] out')).toEqual([3]);
  });
});
