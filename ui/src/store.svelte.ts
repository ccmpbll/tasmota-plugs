// The one list of plugs every part of this module draws from: the Plugs page,
// the section on a printer's page and the line on its card. Each of those reads
// it, and a single timer refreshes it for as long as any of them is on screen,
// so a printers list of ten cards does not read the plugs ten times.
import type { ModuleHost } from '@prusa/connect-ui/slots';

import { hideApplying } from './applying';
import { type Plug, type PlugList, plugsOn, type PlugsService } from './plugs';

// How often the list is read again. A plug can be switched from its own button
// or app, so what was last drawn is not trusted for long.
const REFRESH_MS = 5000;

// A read that fails is not shown as a failure until this many in a row have:
// the module restarts when its settings are saved, and a few seconds of
// "stopping" is not something to alarm the operator with.
const MISSES_BEFORE_FAILURE = 3;
// After a save, how often the list is read while waiting for the restart, and
// for how many tries.
const SETTLE_MS = 1000;
const SETTLE_TRIES = 20;

// Connect takes the page down while the extension restarts and puts a new one
// up after, so what a save started is carried across in the tab's own storage:
// the plugs that were saved, and when.
const PENDING_KEY = 'smart-plugs.pending';
const PENDING_FOR_MS = 120_000;

const rememberSave = (names: readonly string[]): void => {
  try {
    sessionStorage.setItem(
      PENDING_KEY,
      JSON.stringify({ names, at: Date.now() }),
    );
  } catch {
    // No storage: the page just comes back without the note.
  }
};

const forgetSave = (): void => {
  try {
    sessionStorage.removeItem(PENDING_KEY);
  } catch {
    // Nothing was kept.
  }
};

const savedNames = (): string[] | undefined => {
  try {
    const kept: unknown = JSON.parse(
      sessionStorage.getItem(PENDING_KEY) ?? 'null',
    );
    if (typeof kept !== 'object' || kept === null) return undefined;
    const { names, at } = kept as { names?: unknown; at?: unknown };
    if (
      !Array.isArray(names) ||
      !names.every((n): n is string => typeof n === 'string') ||
      typeof at !== 'number' ||
      Date.now() - at > PENDING_FOR_MS
    )
      return undefined;

    return names;
  } catch {
    return undefined;
  }
};

const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms));

const messageOf = (error: unknown): string =>
  error instanceof Error ? error.message : String(error);

export class PlugStore {
  plugs = $state<readonly Plug[]>([]);
  // What a kilowatt-hour costs, when the operator set a price, and its currency.
  pricePerKwh = $state<number | null>(null);
  currency = $state('$');
  // False hides the Turn on and Turn off buttons. Null is unknown, and shows them.
  canOperate = $state<boolean | null>(null);
  loaded = $state(false);

  failure = $state<string | undefined>();
  // True from a save until the module is back with the new settings.
  applying = $state(false);
  // Plain reactive records, by plug name: the host shares `svelte` with a
  // module but not `svelte/reactivity`, which a bundle may not carry itself.
  switching = $state<Record<string, boolean>>({});
  refused = $state<Record<string, string>>({});

  /** Whether to draw the Turn on and Turn off buttons: only a known "no" hides them. */
  get showsControls(): boolean {
    return this.canOperate !== false;
  }

  readonly #signal: AbortSignal;
  readonly #service: PlugsService;
  readonly #t: ModuleHost['t'];
  #timer: ReturnType<typeof setInterval> | undefined;
  #users = 0;
  #misses = 0;
  #reading = false;

  constructor(host: ModuleHost, service: PlugsService = plugsOn(host)) {
    this.#signal = host.signal;
    this.#service = service;
    this.#t = host.t;
  }

  /**
   * Starts the refresh while something is drawn and returns what stops it. The
   * timer runs once however many parts of the module call this.
   */
  use(): () => void {
    this.#users += 1;
    if (this.#users === 1) {
      const saved = savedNames();
      if (saved) void this.settle(saved, false);
      else void this.refresh();
      // A tick is skipped while the last read is still out: a plug that never
      // answers would otherwise have a new read start behind each unfinished one.
      this.#timer = setInterval(() => {
        if (!this.#reading) void this.refresh();
      }, REFRESH_MS);
    }

    let stopped = false;

    return () => {
      if (stopped) return;
      stopped = true;
      this.#users -= 1;
      if (this.#users === 0 && this.#timer !== undefined) {
        clearInterval(this.#timer);
        this.#timer = undefined;
      }
    };
  }

  // What a good read leaves behind.
  #take(list: PlugList): void {
    this.plugs = list.items;
    this.pricePerKwh = list.pricePerKwh;
    this.currency = list.currency;
    this.canOperate = list.canOperate;
    this.failure = undefined;
    this.#misses = 0;
    this.loaded = true;
  }

  async refresh(): Promise<void> {
    this.#reading = true;

    try {
      this.#take(await this.#service.list(this.#signal));
    } catch (error) {
      if (!this.#signal.aborted) {
        // The last list stays on screen through a miss or two; a failure is
        // shown once reads keep failing, or at once if nothing was ever read.
        this.#misses += 1;
        if (!this.loaded || this.#misses >= MISSES_BEFORE_FAILURE)
          this.failure = messageOf(error);
      }
    } finally {
      this.#reading = false;
      this.loaded = true;
    }
  }

  /**
   * Called after settings are saved: the module restarts to take them, so read
   * quietly until it answers with the plugs that were saved. `fresh` is true
   * for the save just made, which is also kept so the page that Connect puts up
   * after the restart carries on.
   */
  async settle(names: readonly string[], fresh = true): Promise<void> {
    if (fresh) rememberSave(names);
    this.applying = true;
    const want = [...names].sort().join('\n');
    let why = this.#t('smart-plugs.page.not_applied');

    try {
      for (let tries = 0; tries < SETTLE_TRIES; tries += 1) {
        if (tries > 0 || fresh) await sleep(SETTLE_MS);
        if (this.#signal.aborted) return;
        try {
          const list = await this.#service.list(this.#signal);
          if (
            list.items
              .map((p) => p.name)
              .sort()
              .join('\n') === want
          ) {
            this.#take(list);
            forgetSave();
            hideApplying();

            return;
          }
        } catch (error) {
          // Still restarting.
          why = messageOf(error);
        }
      }
    } finally {
      this.applying = false;
    }

    // It did not come back with the plugs that were saved: say so now, and the
    // regular reads clear it if it does come back.
    forgetSave();
    hideApplying();
    this.failure = why;
  }

  async setPower(plug: Plug, on: boolean, force = false): Promise<void> {
    this.switching[plug.name] = true;
    this.refused[plug.name] = '';

    try {
      await this.#service.setPower(plug.name, on, force, this.#signal);
    } catch (error) {
      if (!this.#signal.aborted) this.refused[plug.name] = messageOf(error);
    } finally {
      this.switching[plug.name] = false;
    }

    await this.refresh();
  }
}

let held: { host: ModuleHost; store: PlugStore } | undefined;

/** The store of the host this module was handed. A new host gets a new store. */
export const storeFor = (host: ModuleHost): PlugStore => {
  if (held?.host !== host) held = { host, store: new PlugStore(host) };

  return held.store;
};
