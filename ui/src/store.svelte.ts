// The one list of plugs every part of this module draws from: the Plugs page,
// the section on a printer's page and the line on its card. Each of those reads
// it, and a single timer refreshes it for as long as any of them is on screen,
// so a printers list of ten cards does not read the plugs ten times.
import type { ModuleHost } from '@prusa/connect-ui/slots';

import { type Plug, plugsOn, type PlugsService } from './plugs';

// How often the list is read again. A plug can be switched from its own button
// or app, so what was last drawn is not trusted for long.
const REFRESH_MS = 5000;

const messageOf = (error: unknown): string =>
  error instanceof Error ? error.message : String(error);

export class PlugStore {
  plugs = $state<readonly Plug[]>([]);
  // What a kilowatt-hour costs, when the operator set a price, and its currency.
  pricePerKwh = $state<number | null>(null);
  currency = $state('$');
  loaded = $state(false);
  failure = $state<string | undefined>();
  // Plain reactive records, by plug name: the host shares `svelte` with a
  // module but not `svelte/reactivity`, which a bundle may not carry itself.
  switching = $state<Record<string, boolean>>({});
  refused = $state<Record<string, string>>({});

  readonly #signal: AbortSignal;
  readonly #service: PlugsService;
  #timer: ReturnType<typeof setInterval> | undefined;
  #users = 0;

  constructor(host: ModuleHost, service: PlugsService = plugsOn(host)) {
    this.#signal = host.signal;
    this.#service = service;
  }

  /**
   * Starts the refresh while something is drawn and returns what stops it. The
   * timer runs once however many parts of the module call this.
   */
  use(): () => void {
    this.#users += 1;
    if (this.#users === 1) {
      void this.refresh();
      this.#timer = setInterval(() => void this.refresh(), REFRESH_MS);
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

  async refresh(): Promise<void> {
    try {
      const list = await this.#service.list(this.#signal);
      this.plugs = list.items;
      this.pricePerKwh = list.pricePerKwh;
      this.currency = list.currency;
      this.failure = undefined;
    } catch (error) {
      if (!this.#signal.aborted) this.failure = messageOf(error);
    } finally {
      this.loaded = true;
    }
  }

  async setPower(plug: Plug, on: boolean): Promise<void> {
    this.switching[plug.name] = true;
    this.refused[plug.name] = '';

    try {
      await this.#service.setPower(plug.name, on, this.#signal);
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
