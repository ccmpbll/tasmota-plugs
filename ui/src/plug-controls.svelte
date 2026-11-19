<script lang="ts">
  import { printerStateOf, STATE_PATH } from '@prusa/connect-ui/domain';
  import {
    Alert,
    AlertDescription,
    Badge,
    Button,
    ConfirmDialog,
  } from '@prusa/connect-ui/primitives';

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

  // A printer in one of these states loses its print when its power goes.
  const LOSES_A_PRINT = new Set(['PRINTING', 'PAUSED']);

  const device = $derived(
    plug.printer
      ? host.fleet.devices.find((d) => d.serial === plug.printer)
      : undefined,
  );
  const printing = $derived.by(() => {
    const word = device?.state[STATE_PATH];

    return typeof word === 'string' && LOSES_A_PRINT.has(printerStateOf(word));
  });
  const busy = $derived(store.switching[plug.name] === true);
  const refusal = $derived(store.refused[plug.name]);
</script>

<div class="flex flex-col gap-2">
  <div class="flex items-center gap-3">
    {#if plug.on === null}
      <Badge variant="stateFault">
        {t('smart-plugs.page.state.unreachable')}
      </Badge>
    {:else if plug.on}
      <Badge variant="success">{t('smart-plugs.page.state.on')}</Badge>
    {:else}
      <Badge variant="secondary">{t('smart-plugs.page.state.off')}</Badge>
    {/if}

    {#if plug.on !== true}
      <Button
        size="sm"
        pending={busy}
        disabled={busy}
        onclick={() => void store.setPower(plug, true)}
      >
        {t('smart-plugs.page.turn_on')}
      </Button>
    {/if}

    <!-- Nothing to turn off when it is already off. -->
    {#if plug.on !== false && printing}
      <ConfirmDialog
        variant="destructive"
        title={t('smart-plugs.page.confirm.title', { name: plug.name })}
        description={t('smart-plugs.page.confirm.description', {
          printer: device?.name ?? plug.printer ?? plug.name,
        })}
        confirmLabel={t('smart-plugs.page.turn_off')}
        onConfirm={() => void store.setPower(plug, false)}
      >
        {#snippet trigger({ props })}
          <Button {...props} size="sm" variant="destructive" disabled={busy}>
            {t('smart-plugs.page.turn_off')}
          </Button>
        {/snippet}
      </ConfirmDialog>
    {:else if plug.on !== false}
      <Button
        size="sm"
        variant="destructive"
        pending={busy}
        disabled={busy}
        onclick={() => void store.setPower(plug, false)}
      >
        {t('smart-plugs.page.turn_off')}
      </Button>
    {/if}
  </div>

  {#if plug.error}
    <p class="text-xs text-muted-foreground">{plug.error}</p>
  {/if}
  {#if refusal}
    <Alert variant="destructive">
      <AlertDescription>
        {t('smart-plugs.page.refused', { reason: refusal })}
      </AlertDescription>
    </Alert>
  {/if}
</div>
