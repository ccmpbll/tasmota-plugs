<script lang="ts">
  import { Card, CardContent } from '@prusa/connect-ui/primitives';

  import { heldHost } from './host';
  import PlugControls from './plug-controls.svelte';
  import PlugEnergy from './plug-energy.svelte';
  import { storeFor } from './store.svelte';

  interface Props {
    deviceId: string;
  }

  let { deviceId }: Props = $props();

  const host = heldHost();
  const t = host.t;
  const store = storeFor(host);

  // The plugs that power this printer, matched by its serial.
  const serial = $derived(host.fleet.device(deviceId)?.serial);
  const mine = $derived(
    serial ? store.plugs.filter((plug) => plug.printer === serial) : [],
  );

  // A printer with no plug gets a link to set one up, but only once the module is
  // in use (some plug exists), so a server that does not use plugs shows nothing.
  const offerSetup = $derived(
    mine.length === 0 &&
      serial !== undefined &&
      serial !== '' &&
      store.plugs.length > 0,
  );
  const setupHref = $derived(
    `${host.pageHref('smart-plugs')}?add=${encodeURIComponent(serial ?? '')}`,
  );

  $effect(() => store.use());
</script>

{#if mine.length > 0}
  <Card>
    <CardContent class="flex flex-col gap-4">
      {#each mine as plug (plug.name)}
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="flex min-w-0 flex-col gap-2">
            <div class="flex flex-col gap-1">
              <span class="text-sm font-medium">{plug.name}</span>
              <span class="text-xs text-muted-foreground">{plug.host}</span>
            </div>
            <PlugEnergy {plug} compact />
          </div>
          <PlugControls {plug} />
        </div>
      {/each}
    </CardContent>
  </Card>
{:else if offerSetup}
  <p class="text-sm text-muted-foreground">
    <a class="underline-offset-4 hover:underline" href={setupHref}>
      {t('smart-plugs.section.setup')}
    </a>
  </p>
{/if}
