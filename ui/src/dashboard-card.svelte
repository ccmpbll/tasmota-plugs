<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';
  import { TilePanel } from '@prusa/connect-ui/primitives';
  import type { CardProps } from '@prusa/connect-ui/slots';

  import { heldHost } from './host';
  import PlugControls from './plug-controls.svelte';
  import type { Plug } from './plugs';
  import { storeFor } from './store.svelte';

  // A dashboard card: a row per plug with its state and a button to switch it.
  // The card's one setting narrows it to the plugs of the printers a person
  // chose, and shows every plug when none are chosen.
  let { config, title, editing }: CardProps = $props();

  const host = heldHost();
  const t = host.t;
  const store = storeFor(host);

  const chosen = $derived(
    Array.isArray(config.printers)
      ? config.printers.filter((id): id is string => typeof id === 'string')
      : [],
  );
  const serials = $derived(
    chosen
      .map((id) => host.fleet.device(id)?.serial)
      .filter((serial) => serial !== undefined && serial !== ''),
  );
  const shown = $derived(
    chosen.length === 0
      ? store.plugs
      : store.plugs.filter(
          (plug) =>
            plug.printer !== undefined && serials.includes(plug.printer),
        ),
  );

  const heading = $derived(
    title.trim() === '' ? t('smart-plugs.cards.plugs.heading') : title,
  );

  // What sits under a plug's name: the printer it powers and what it is drawing.
  const detail = (plug: Plug): string =>
    [
      plug.printer === undefined
        ? undefined
        : (host.fleet.devices.find((d) => d.serial === plug.printer)?.name ??
          plug.printer),
      plug.energy?.watts === undefined
        ? undefined
        : t('smart-plugs.energy.watts', {
            watts: formatNumber(plug.energy.watts, {
              maximumFractionDigits: 0,
            }),
          }),
    ]
      .filter((part) => part !== undefined)
      .join(' · ');

  $effect(() => store.use());
</script>

<TilePanel title={heading}>
  <div class="flex min-h-0 flex-col gap-3 overflow-y-auto">
    {#each shown as plug (plug.name)}
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex min-w-0 flex-col">
          <span class="truncate text-sm">{plug.name}</span>
          <span class="truncate text-xs text-muted-foreground"
            >{detail(plug)}</span
          >
        </div>
        <!-- While the dashboard is being rearranged the buttons are inert, so a
             click that meant to move the card cannot cut a printer's power. -->
        <div class={editing ? 'pointer-events-none opacity-60' : ''}>
          <PlugControls {plug} />
        </div>
      </div>
    {/each}

    {#if store.loaded && shown.length === 0}
      <p class="text-sm text-muted-foreground">
        {t('smart-plugs.cards.plugs.empty')}
      </p>
    {/if}
  </div>
</TilePanel>
