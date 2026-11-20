import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ModuleHost } from '@prusa/connect-ui/slots';

import type { Plug, PlugList, PlugsService } from './plugs';
import { PlugStore } from './store.svelte';

vi.mock('./applying', () => ({
  hideApplying: vi.fn(),
  showApplying: vi.fn(),
}));

import { hideApplying } from './applying';

const KEY = 'smart-plugs.pending';

const plug = (name: string): Plug => ({ name, host: `${name}.lan`, on: true });
const list = (...names: string[]): PlugList => ({
  items: names.map(plug),
  pricePerKwh: null,
  currency: '$',
  canOperate: null,
});

// sessionStorage as a Map, which is all the store asks of it.
const storage = new Map<string, string>();

const host = { signal: new AbortController().signal, t: (key: string) => key };

// A service whose reads are the given answers in turn, then the last one again.
const serving = (...answers: (PlugList | Error)[]) => {
  let at = 0;
  const reads = vi.fn(() => {
    const answer =
      answers[Math.min(at, answers.length - 1)] ?? new Error('no answer');
    at += 1;

    return answer instanceof Error
      ? Promise.reject(answer)
      : Promise.resolve(answer);
  });
  const service: PlugsService = {
    list: reads,
    setPower: () => Promise.resolve(),
  };

  return { service, reads };
};

const storeOn = (service: PlugsService) =>
  new PlugStore(host as unknown as ModuleHost, service);

beforeEach(() => {
  vi.useFakeTimers();
  storage.clear();
  vi.stubGlobal('sessionStorage', {
    getItem: (k: string) => storage.get(k) ?? null,
    setItem: (k: string, v: string) => void storage.set(k, v),
    removeItem: (k: string) => void storage.delete(k),
  });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('who may operate', () => {
  it('follows the list: unknown shows the buttons, only false hides them', async () => {
    const answer = (canOperate: boolean | null): PlugList => ({
      ...list('a'),
      canOperate,
    });
    const store = storeOn(
      serving(answer(null), answer(false), answer(true)).service,
    );

    await store.refresh();
    expect(store.canOperate).toBeNull();
    expect(store.showsControls).toBe(true);

    await store.refresh();
    expect(store.canOperate).toBe(false);
    expect(store.showsControls).toBe(false);

    await store.refresh();
    expect(store.canOperate).toBe(true);
    expect(store.showsControls).toBe(true);
  });
});

describe('a read that fails', () => {
  it('keeps the plugs on screen and shows a failure only after three in a row', async () => {
    const { service } = serving(list('a'), new Error('down'));
    const store = storeOn(service);

    await store.refresh();
    expect(store.plugs.map((p) => p.name)).toEqual(['a']);

    await store.refresh();
    await store.refresh();
    expect(store.failure).toBeUndefined();
    expect(store.plugs).toHaveLength(1);

    await store.refresh();
    expect(store.failure).toBe('down');
  });

  it('is shown at once when nothing was ever read', async () => {
    const store = storeOn(serving(new Error('down')).service);

    await store.refresh();
    expect(store.failure).toBe('down');
  });

  it('is cleared by the next good read, and the count starts again', async () => {
    const store = storeOn(
      serving(
        list('a'),
        new Error('x'),
        new Error('x'),
        list('a'),
        new Error('x'),
      ).service,
    );

    for (let i = 0; i < 5; i += 1) await store.refresh();
    expect(store.failure).toBeUndefined();
  });
});

describe('settling after a save', () => {
  it('waits through the restart and ends on the saved plugs', async () => {
    const { service } = serving(
      list('old'),
      new Error('stopping'),
      list('old', 'new'),
    );
    const store = storeOn(service);

    const done = store.settle(['old', 'new']);
    expect(store.applying).toBe(true);
    expect(storage.has(KEY)).toBe(true);

    await vi.advanceTimersByTimeAsync(5000);
    await done;

    expect(store.applying).toBe(false);
    expect(store.plugs.map((p) => p.name)).toEqual(['old', 'new']);
    expect(store.failure).toBeUndefined();
    expect(storage.has(KEY)).toBe(false);
    expect(hideApplying).toHaveBeenCalled();
  });

  it('says so when the extension never answers', async () => {
    const store = storeOn(serving(new Error('refused')).service);

    const done = store.settle(['a']);
    await vi.advanceTimersByTimeAsync(60_000);
    await done;

    expect(store.applying).toBe(false);
    expect(store.failure).toBe('refused');
    expect(storage.has(KEY)).toBe(false);
  });

  it('says so when it answers with other plugs than were saved', async () => {
    const store = storeOn(serving(list('old')).service);

    const done = store.settle(['new']);
    await vi.advanceTimersByTimeAsync(60_000);
    await done;

    expect(store.failure).toBe('smart-plugs.page.not_applied');
  });
});

describe('a page that Connect puts up after the restart', () => {
  it('carries on with a save made a moment ago', async () => {
    storage.set(KEY, JSON.stringify({ names: ['a'], at: Date.now() }));
    const store = storeOn(serving(new Error('stopping'), list('a')).service);

    const stop = store.use();
    expect(store.applying).toBe(true);

    await vi.advanceTimersByTimeAsync(3000);
    expect(store.applying).toBe(false);
    expect(store.plugs.map((p) => p.name)).toEqual(['a']);
    stop();
  });

  it('ignores a save older than two minutes', async () => {
    storage.set(
      KEY,
      JSON.stringify({ names: ['gone'], at: Date.now() - 121_000 }),
    );
    const store = storeOn(serving(list('a')).service);

    const stop = store.use();
    await vi.advanceTimersByTimeAsync(10);

    expect(store.applying).toBe(false);
    expect(store.plugs.map((p) => p.name)).toEqual(['a']);
    stop();
  });

  it('ignores a note that is not one', async () => {
    storage.set(KEY, '{"names":[1],"at":"x"}');
    const store = storeOn(serving(list('a')).service);

    const stop = store.use();
    await vi.advanceTimersByTimeAsync(10);

    expect(store.applying).toBe(false);
    stop();
  });
});

describe('the regular read', () => {
  it('skips a tick while the last read is still out', async () => {
    let finish: (l: PlugList) => void = () => undefined;
    const reads = vi.fn(
      () =>
        new Promise<PlugList>((resolve) => {
          finish = resolve;
        }),
    );
    const store = storeOn({ list: reads, setPower: () => Promise.resolve() });

    const stop = store.use();
    expect(reads).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(15_000);
    expect(reads).toHaveBeenCalledTimes(1);

    finish(list('a'));
    await vi.advanceTimersByTimeAsync(5000);
    expect(reads).toHaveBeenCalledTimes(2);
    stop();
  });
});
