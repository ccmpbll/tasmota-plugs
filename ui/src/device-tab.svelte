<script lang="ts">
  import { Card, CardContent } from '@prusa/connect-ui/primitives';

  import { heldHost } from './host';
  import PlugCard from './plug-card.svelte';
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
  const setupHref = $derived(
    `${host.pageHref('smart-plugs')}?add=${encodeURIComponent(serial ?? '')}`,
  );

  $effect(() => store.use());
</script>

<div class="flex flex-col gap-4">
  {#each mine as plug (plug.name)}
    <PlugCard {plug} showPrinter={false} />
  {/each}

  {#if mine.length === 0 && store.loaded}
    <Card>
      <CardContent class="flex flex-col gap-2">
        <p class="text-sm text-muted-foreground">{t('smart-plugs.tab.none')}</p>
        {#if serial}
          <a
            class="text-sm underline-offset-4 hover:underline"
            href={setupHref}
          >
            {t('smart-plugs.tab.setup')}
          </a>
        {/if}
      </CardContent>
    </Card>
  {/if}
</div>
