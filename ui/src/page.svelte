<script lang="ts">
  import {
    Alert,
    AlertDescription,
    Button,
    Card,
    CardContent,
    EmptyState,
    PageHeader,
  } from '@prusa/connect-ui/primitives';

  import { heldHost } from './host';
  import PlugControls from './plug-controls.svelte';
  import PlugEnergy from './plug-energy.svelte';
  import PlugSetup from './plug-setup.svelte';
  import { storeFor } from './store.svelte';

  const host = heldHost();
  const t = host.t;
  const store = storeFor(host);

  // A link from a printer's page with no plug opens the editor with a new plug
  // for that printer already chosen: ?add=<serial>.
  const addFor =
    new URLSearchParams(window.location.search).get('add') ?? undefined;
  let editing = $state(addFor !== undefined);

  // The page's own cards say which printer a plug powers by name where the
  // fleet knows it, and by serial where it does not.
  const printerName = (serial: string | undefined): string | undefined =>
    serial
      ? (host.fleet.devices.find((d) => d.serial === serial)?.name ?? serial)
      : undefined;

  $effect(() => store.use());
</script>

<div class="flex flex-col gap-4">
  <PageHeader
    title={t('smart-plugs.page.title')}
    action={editing
      ? undefined
      : { label: t('smart-plugs.setup.open'), onclick: () => (editing = true) }}
  />

  {#if editing}
    <PlugSetup {addFor} onclose={() => (editing = false)} />
  {/if}

  {#if store.failure}
    <Alert variant="destructive">
      <AlertDescription>
        {t('smart-plugs.page.failed', { reason: store.failure })}
      </AlertDescription>
    </Alert>
  {/if}

  {#if store.loaded && store.plugs.length === 0 && !store.failure && !editing}
    <EmptyState
      size="sm"
      heading={t('smart-plugs.page.empty.heading')}
      description={t('smart-plugs.page.empty.description')}
    >
      {#snippet action()}
        <Button size="sm" onclick={() => (editing = true)}>
          {t('smart-plugs.setup.add')}
        </Button>
      {/snippet}
    </EmptyState>
  {/if}

  {#each store.plugs as plug (plug.name)}
    <Card>
      <CardContent>
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="flex min-w-0 flex-col gap-2">
            <div class="flex flex-col gap-1">
              <span class="text-sm font-medium">{plug.name}</span>
              <span class="text-xs text-muted-foreground">
                {plug.host}
                {#if plug.printer}
                  · {printerName(plug.printer)}
                {/if}
              </span>
            </div>
            <PlugEnergy {plug} />
          </div>
          <PlugControls {plug} />
        </div>
      </CardContent>
    </Card>
  {/each}
</div>
