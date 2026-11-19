<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import { heldHost } from './host';
  import type { Plug } from './plugs';
  import { storeFor } from './store.svelte';

  interface Props {
    plug: Plug;
  }

  let { plug }: Props = $props();

  const host = heldHost();
  const t = host.t;
  const store = storeFor(host);

  interface Stat {
    label: string;
    value: string;
    /** What the energy cost, when the operator set a price. */
    cost?: string | undefined;
  }

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

  const energy = (label: string, kwh: number | undefined): Stat | undefined =>
    kwh === undefined
      ? undefined
      : {
          label,
          value: t('smart-plugs.energy.kwh', { kwh: number(kwh, 2) }),
          cost: money(kwh),
        };

  const e = $derived(plug.energy);

  // What the plug is drawing now, then what it has used.
  const live = $derived.by((): Stat[] =>
    [
      e?.watts === undefined
        ? undefined
        : {
            label: t('smart-plugs.energy.label.power'),
            value: t('smart-plugs.energy.watts', { watts: number(e.watts, 0) }),
          },
      e?.volts === undefined
        ? undefined
        : {
            label: t('smart-plugs.energy.label.voltage'),
            value: t('smart-plugs.energy.volts', { volts: number(e.volts, 0) }),
          },
      e?.amps === undefined
        ? undefined
        : {
            label: t('smart-plugs.energy.label.current'),
            value: t('smart-plugs.energy.amps', { amps: number(e.amps, 2) }),
          },
    ].filter((stat) => stat !== undefined),
  );
  const used = $derived.by((): Stat[] =>
    [
      energy(t('smart-plugs.energy.label.today'), e?.today_kwh),
      energy(t('smart-plugs.energy.label.yesterday'), e?.yesterday_kwh),
      energy(t('smart-plugs.energy.label.total'), e?.total_kwh),
    ].filter((stat) => stat !== undefined),
  );
</script>

{#if live.length > 0 || used.length > 0}
  <div class="flex flex-col gap-3">
    {#each [live, used] as row, index (index)}
      {#if row.length > 0}
        <dl class="grid grid-cols-3 gap-x-8 gap-y-1">
          {#each row as stat (stat.label)}
            <div class="flex flex-col">
              <dt class="text-xs text-muted-foreground">{stat.label}</dt>
              <dd class="text-sm">{stat.value}</dd>
              {#if stat.cost}
                <dd class="text-xs text-muted-foreground">{stat.cost}</dd>
              {/if}
            </div>
          {/each}
        </dl>
      {/if}
    {/each}
  </div>
{/if}
