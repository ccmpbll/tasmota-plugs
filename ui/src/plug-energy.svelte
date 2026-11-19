<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import { heldHost } from './host';
  import type { Plug } from './plugs';
  import { storeFor } from './store.svelte';

  interface Props {
    plug: Plug;
    /** Watts and today's energy only, for a printer's own page. */
    compact?: boolean;
  }

  let { plug, compact = false }: Props = $props();

  const host = heldHost();
  const t = host.t;
  const store = storeFor(host);

  const e = $derived(plug.energy);

  const number = (value: number, digits: number): string =>
    formatNumber(value, {
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    });

  // A symbol goes in front of the amount ($0.02), a code after it (0.02 CAD).
  const money = (kwh: number): string | undefined => {
    if (store.pricePerKwh === null) return undefined;
    const amount = number(kwh * store.pricePerKwh, 2);

    return Array.from(store.currency).length === 1
      ? `${store.currency}${amount}`
      : `${amount} ${store.currency}`;
  };

  const energyLine = (
    key: string,
    kwh: number | undefined,
  ): string | undefined => {
    if (kwh === undefined) return undefined;
    const cost = money(kwh);

    return cost === undefined
      ? t(key, { kwh: number(kwh, 2) })
      : t(`${key}_priced`, { kwh: number(kwh, 2), cost });
  };

  const live = $derived(
    [
      e?.watts === undefined
        ? undefined
        : t('smart-plugs.energy.watts', { watts: number(e.watts, 0) }),
      compact || e?.volts === undefined
        ? undefined
        : t('smart-plugs.energy.volts', { volts: number(e.volts, 0) }),
      compact || e?.amps === undefined
        ? undefined
        : t('smart-plugs.energy.amps', { amps: number(e.amps, 2) }),
    ].filter((part) => part !== undefined),
  );
  const totals = $derived(
    [
      energyLine('smart-plugs.energy.today', e?.today_kwh),
      compact
        ? undefined
        : energyLine('smart-plugs.energy.yesterday', e?.yesterday_kwh),
      compact
        ? undefined
        : energyLine('smart-plugs.energy.total', e?.total_kwh),
    ].filter((part) => part !== undefined),
  );
  const signal = $derived(
    compact || plug.signal_dbm === undefined
      ? undefined
      : t('smart-plugs.energy.signal', { dbm: plug.signal_dbm }),
  );
</script>

{#if live.length > 0 || totals.length > 0}
  <div class="flex flex-col gap-0.5 text-xs text-muted-foreground">
    {#if live.length > 0}
      <span class="text-sm text-foreground">{live.join(' · ')}</span>
    {/if}
    {#if totals.length > 0}
      <span>{totals.join(' · ')}</span>
    {/if}
    {#if signal}
      <span>{signal}</span>
    {/if}
  </div>
{/if}
