<script lang="ts">
  import { Card, CardContent } from '@prusa/connect-ui/primitives';

  import { heldHost } from './host';
  import PlugControls from './plug-controls.svelte';
  import PlugEnergy from './plug-energy.svelte';
  import type { Plug } from './plugs';

  interface Props {
    plug: Plug;
    /** Name the printer the plug powers. Off on that printer's own page. */
    showPrinter?: boolean;
  }

  let { plug, showPrinter = true }: Props = $props();

  const host = heldHost();
  const t = host.t;

  // The printer by name where the fleet knows it, and by serial where it does not.
  const printer = $derived(
    plug.printer
      ? (host.fleet.devices.find((d) => d.serial === plug.printer)?.name ??
          plug.printer)
      : undefined,
  );
</script>

<Card>
  <CardContent>
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="flex min-w-0 flex-col gap-3">
        <div class="flex flex-col gap-1">
          <span class="text-sm font-medium">{plug.name}</span>
          <span class="text-xs text-muted-foreground">
            {plug.host}
            {#if showPrinter && printer}
              · {printer}
            {/if}
          </span>
        </div>
        <PlugEnergy {plug} />
        {#if plug.auto_off && plug.on === false}
          <p class="text-xs text-muted-foreground">
            {t(`smart-plugs.card.auto_off.${plug.auto_off.reason}`, {
              time: new Date(plug.auto_off.at).toLocaleString([], {
                dateStyle: 'short',
                timeStyle: 'short',
              }),
              detail: plug.auto_off.detail,
            })}
          </p>
        {/if}
      </div>
      <PlugControls {plug} />
    </div>
  </CardContent>
</Card>
