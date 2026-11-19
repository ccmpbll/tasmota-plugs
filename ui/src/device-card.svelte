<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import { heldHost } from './host';
  import { storeFor } from './store.svelte';

  interface Props {
    deviceId: string;
  }

  let { deviceId }: Props = $props();

  const host = heldHost();
  const t = host.t;
  const store = storeFor(host);

  const serial = $derived(host.fleet.device(deviceId)?.serial);
  const mine = $derived(
    serial ? store.plugs.filter((plug) => plug.printer === serial) : [],
  );

  const word = (on: boolean | null): string => {
    if (on === null) return t('smart-plugs.page.state.unreachable');

    return on
      ? t('smart-plugs.page.state.on')
      : t('smart-plugs.page.state.off');
  };

  // The power the printer's plugs draw together, when each of them has a meter.
  const watts = $derived.by(() => {
    const readings = mine.map((plug) => plug.energy?.watts);
    if (readings.length === 0 || readings.some((w) => w === undefined))
      return undefined;

    return readings.reduce<number>((sum, w) => sum + (w ?? 0), 0);
  });

  // One short line: "Power: On · 15 W", or each plug's name and state when a
  // printer has several. The card cuts text past one line.
  const state = $derived(
    mine.length === 1
      ? word(mine[0]?.on ?? null)
      : mine.map((plug) => `${plug.name} ${word(plug.on)}`).join(' · '),
  );
  const line = $derived(
    watts === undefined
      ? state
      : `${state} · ${t('smart-plugs.energy.watts', { watts: formatNumber(watts, { maximumFractionDigits: 0 }) })}`,
  );

  $effect(() => store.use());
</script>

{#if mine.length > 0}
  <span class="text-sm text-muted-foreground">
    {t('smart-plugs.card.line', { state: line })}
  </span>
{/if}
