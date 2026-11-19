<script lang="ts">
  import {
    Alert,
    AlertDescription,
    Button,
    EmptyState,
    PageHeader,
  } from '@prusa/connect-ui/primitives';

  import { heldHost } from './host';
  import PlugCard from './plug-card.svelte';
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
    <PlugCard {plug} />
  {/each}
</div>
