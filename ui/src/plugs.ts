// The service layer: the module's own routes, reached through the host's
// client. `bind` comes from `smart-plugs-client`, which
// `cmd/generate-client-sdk -module` wrote from openapi.json (scripts/ui-build.sh
// runs it). The host's client is in session mode, so a call carries the cookie
// the browser already holds, and nothing here names a URL, a header or a token.
import type { ModuleHost } from '@prusa/connect-ui/slots';
import { bind } from 'smart-plugs-client';

/** What a plug's energy meter reports. A field the plug does not send is absent. */
export interface Energy {
  readonly watts?: number | undefined;
  readonly volts?: number | undefined;
  readonly amps?: number | undefined;
  readonly power_factor?: number | undefined;
  readonly today_kwh?: number | undefined;
  readonly yesterday_kwh?: number | undefined;
  readonly total_kwh?: number | undefined;
}

/** One plug as the list route answers it. `on` is null when it could not be read. */
export interface Plug {
  readonly name: string;
  readonly host: string;
  /** The serial of the printer it powers, when one is linked. */
  readonly printer?: string | undefined;
  readonly on: boolean | null;
  /** Why the plug could not be read, when it could not. */
  readonly error?: string | undefined;
  /** The plug's meter, when it has one. */
  readonly energy?: Energy | undefined;
  readonly signal_dbm?: number | undefined;
  readonly uptime_s?: number | undefined;
}

/** The plugs, and what a kilowatt-hour costs so the energy can be shown as money. */
export interface PlugList {
  readonly items: readonly Plug[];
  readonly pricePerKwh: number | null;
  readonly currency: string;
}

/** What the page needs of the module's routes. A test hands in a fake. */
export interface PlugsService {
  list(signal?: AbortSignal): Promise<PlugList>;
  setPower(name: string, on: boolean, signal?: AbortSignal): Promise<void>;
}

/** The routes, bound onto the client the host handed this module. */
export const plugsOn = (host: ModuleHost): PlugsService => {
  const routes = bind(host.client);

  return {
    list: async (signal) => {
      const answer = await routes.listPlugs({}, signal);

      return {
        items: answer.items,
        pricePerKwh: answer.price_per_kwh ?? null,
        currency: answer.currency,
      };
    },
    setPower: async (name, on, signal) => {
      await routes.setPlugPower({ path: { name }, body: { on } }, signal);
    },
  };
};
