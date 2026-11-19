<script lang="ts">
  import {
    Alert,
    AlertDescription,
    Button,
    Card,
    CardContent,
    Input,
    Label,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
  } from '@prusa/connect-ui/primitives';

  import { showApplying } from './applying';
  import { heldHost } from './host';
  import { storeFor } from './store.svelte';

  interface Props {
    /** Closes the editor, saved or not. */
    onclose: () => void;
    /** A printer serial to start a new plug for, from a link on its page. */
    addFor?: string | undefined;
  }

  let { onclose, addFor }: Props = $props();

  const host = heldHost();
  const t = host.t;
  const store = storeFor(host);

  // The namespace Connect derives for this module, which is how its instance is
  // found among the server's resources whatever the operator named it.
  const NAMESPACE = 'com.github.aux.ccmpbll.prusa-connect-tasmota';
  // The module reads at most this many plugs: each host is a connection it
  // declares to the operator, one setting per slot.
  const MAX_PLUGS = 8;
  const KIND = 'sink';
  const NONE = 'none';
  const DEFAULT_CURRENCY = '$';

  // The same rules the module holds the settings to at start, so a mistake is
  // found here and not as a refused start.
  const NAME = /^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$/;
  const LABEL = '[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?';
  const HOST = new RegExp(`^${LABEL}(\\.${LABEL})*$`);

  interface Row {
    readonly id: number;
    name: string;
    host: string;
    printer: string;
  }

  type Resource = Awaited<ReturnType<typeof host.client.getResource>>;

  let nextId = 0;
  let rows = $state<Row[]>([]);
  let price = $state('');
  let currency = $state(DEFAULT_CURRENCY);
  let loading = $state(true);
  let saving = $state(false);
  let failure = $state<string | undefined>();
  // The resource as it was read, so a save sends back what it did not change.
  let resource = $state<Resource | undefined>();

  // What an input holds, read off the event that says it changed.
  const typed = (event: Event): string =>
    event.target instanceof HTMLInputElement ? event.target.value : '';

  const messageOf = (error: unknown): string =>
    error instanceof Error ? error.message : String(error);

  const word = (
    config: Readonly<Record<string, unknown>>,
    key: string,
  ): string => {
    const value = config[key];

    return typeof value === 'string' ? value : '';
  };

  const blank = (printer = ''): Row => ({
    id: nextId++,
    name: '',
    host: '',
    printer,
  });

  const load = async (): Promise<void> => {
    try {
      const listed = await host.client.listResources({}, host.signal);
      // This module is a sink, so its instance is one the server lists as such.
      const found = (listed.items ?? []).find(
        (r) => r.namespace === NAMESPACE && r.kind === KIND,
      );
      if (!found) throw new Error(t('smart-plugs.setup.error.not_found'));

      const spec = await host.client.getResource(
        { path: { kind: KIND, name: found.name } },
        host.signal,
      );
      const config: Readonly<Record<string, unknown>> = spec.config;

      const read: Row[] = [];
      for (let slot = 1; slot <= MAX_PLUGS; slot += 1) {
        const row = {
          id: nextId++,
          name: word(config, `plug${String(slot)}_name`),
          host: word(config, `plug${String(slot)}_host`),
          printer: word(config, `plug${String(slot)}_printer`),
        };
        if (row.name !== '' || row.host !== '' || row.printer !== '')
          read.push(row);
      }

      if (addFor !== undefined && read.length < MAX_PLUGS)
        read.push(blank(addFor));

      resource = spec;
      rows = read;
      price = word(config, 'price_per_kwh');
      currency = word(config, 'currency') || DEFAULT_CURRENCY;
    } catch (error) {
      if (!host.signal.aborted)
        failure = t('smart-plugs.setup.error.load', {
          reason: messageOf(error),
        });
    } finally {
      loading = false;
    }
  };

  void load();

  // The printers Connect knows, by serial. A plug linked to a serial that is not
  // among them (a printer that is retired, or not connected yet) is kept, and
  // labelled so.
  const printers = $derived(
    host.fleet.devices
      .filter((d) => d.serial !== '')
      .map((d) => ({
        value: d.serial,
        label: `${d.name} · ${d.serial}`,
      })),
  );

  const optionsFor = (serial: string) => [
    { value: NONE, label: t('smart-plugs.setup.printer.none') },
    ...printers,
    ...(serial !== '' && !printers.some((p) => p.value === serial)
      ? [
          {
            value: serial,
            label: t('smart-plugs.setup.printer.gone', { serial }),
          },
        ]
      : []),
  ];

  // The name a plug is saved under: what was typed, or plug<slot>.
  const effectiveName = (row: Row, index: number): string =>
    row.name.trim() || `plug${String(index + 1)}`;

  // Each row with its position and what is wrong with it, so the template reads
  // a row's problems from the row and never by index.
  const view = $derived(
    rows.map((row, index) => {
      const name = effectiveName(row, index);
      const duplicate = rows.some(
        (other, at) => at !== index && effectiveName(other, at) === name,
      );
      let nameProblem: string | undefined;
      if (!NAME.test(name)) nameProblem = t('smart-plugs.setup.error.name');
      else if (duplicate)
        nameProblem = t('smart-plugs.setup.error.duplicate', { name });

      const address = row.host.trim();
      const hostProblem =
        HOST.test(address) && address.length <= 253
          ? undefined
          : t('smart-plugs.setup.error.host');

      return { row, index, nameProblem, hostProblem };
    }),
  );

  const priceProblem = $derived.by(() => {
    const text = price.trim();
    if (text === '') return undefined;
    const value = Number(text);

    return Number.isFinite(value) && value >= 0 && value <= 1000
      ? undefined
      : t('smart-plugs.setup.error.price');
  });
  const currencyProblem = $derived(
    Array.from(currency.trim()).length > 8 || /\s/.test(currency.trim())
      ? t('smart-plugs.setup.error.currency')
      : undefined,
  );

  const valid = $derived(
    view.every(
      (v) => v.nameProblem === undefined && v.hostProblem === undefined,
    ) &&
      priceProblem === undefined &&
      currencyProblem === undefined,
  );

  const add = (): void => {
    if (rows.length < MAX_PLUGS) rows.push(blank());
  };

  const remove = (id: number): void => {
    rows = rows.filter((row) => row.id !== id);
  };

  const save = async (): Promise<void> => {
    if (!resource || !valid || saving) return;
    saving = true;
    failure = undefined;

    // Every slot is written, so a slot that was removed is cleared.
    const settings: Record<string, string> = {};
    for (let slot = 1; slot <= MAX_PLUGS; slot += 1) {
      const row = rows[slot - 1];
      settings[`plug${String(slot)}_name`] = row
        ? effectiveName(row, slot - 1)
        : '';
      settings[`plug${String(slot)}_host`] = row?.host.trim() ?? '';
      settings[`plug${String(slot)}_printer`] = row?.printer.trim() ?? '';
    }
    settings.price_per_kwh = price.trim();
    settings.currency = currency.trim() || DEFAULT_CURRENCY;

    try {
      await host.client.putResource(
        {
          path: { kind: KIND, name: resource.name },
          // The write is refused without the version it was read at, so two
          // people editing the same instance cannot overwrite each other.
          header: { 'If-Match': `"${resource.version}"` },
          body: {
            type: resource.type,
            enabled: resource.enabled,
            required: resource.required,
            config: { ...resource.config, ...settings },
          },
        },
        host.signal,
      );
      // The module restarts to take the new settings. Close at once and let
      // the store wait for it quietly, with the page saying so meanwhile.
      showApplying(
        t('smart-plugs.applying.title'),
        t('smart-plugs.applying.detail'),
      );
      void store.settle(rows.map((row, index) => effectiveName(row, index)));
      onclose();
    } catch (error) {
      if (!host.signal.aborted)
        failure = t('smart-plugs.setup.error.save', {
          reason: messageOf(error),
        });
    } finally {
      saving = false;
    }
  };
</script>

<Card>
  <CardContent class="flex flex-col gap-5">
    <div class="flex flex-col gap-1">
      <h2 class="text-base font-medium">{t('smart-plugs.setup.title')}</h2>
      <p class="text-sm text-muted-foreground">
        {t('smart-plugs.setup.description')}
      </p>
    </div>

    {#if loading}
      <p class="text-sm text-muted-foreground">
        {t('smart-plugs.setup.loading')}
      </p>
    {/if}

    {#if failure}
      <Alert variant="destructive">
        <AlertDescription>{failure}</AlertDescription>
      </Alert>
    {/if}

    {#if resource}
      <div class="flex flex-col gap-4">
        {#each view as { row, index, nameProblem, hostProblem } (row.id)}
          <div
            class="grid gap-3 border-b pb-4 md:grid-cols-4 md:items-start md:border-b-0 md:pb-0"
          >
            <div class="flex flex-col gap-1">
              <Label for={`name-${String(row.id)}`}
                >{t('smart-plugs.setup.name')}</Label
              >
              <Input
                id={`name-${String(row.id)}`}
                value={row.name}
                oninput={(event: Event) => (row.name = typed(event))}
                placeholder={t('smart-plugs.setup.name.placeholder', {
                  n: index + 1,
                })}
                aria-invalid={nameProblem !== undefined}
              />
              {#if nameProblem}
                <p class="text-xs text-destructive-text">
                  {nameProblem}
                </p>
              {/if}
            </div>

            <div class="flex flex-col gap-1">
              <Label for={`host-${String(row.id)}`}
                >{t('smart-plugs.setup.host')}</Label
              >
              <Input
                id={`host-${String(row.id)}`}
                value={row.host}
                oninput={(event: Event) => (row.host = typed(event))}
                placeholder={t('smart-plugs.setup.host.placeholder')}
                aria-invalid={hostProblem !== undefined}
              />
              {#if hostProblem}
                <p class="text-xs text-destructive-text">
                  {hostProblem}
                </p>
              {/if}
            </div>

            <div class="flex flex-col gap-1">
              <Label>{t('smart-plugs.setup.printer')}</Label>
              <Select
                value={row.printer === '' ? NONE : row.printer}
                items={optionsFor(row.printer)}
                onValueChange={(value: string) =>
                  (row.printer = value === NONE ? '' : value)}
              >
                <SelectTrigger
                  aria-label={t('smart-plugs.setup.printer')}
                  class="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {#each optionsFor(row.printer) as option (option.value)}
                    <SelectItem value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  {/each}
                </SelectContent>
              </Select>
            </div>

            <div class="md:pt-6">
              <Button variant="ghost" size="sm" onclick={() => remove(row.id)}>
                {t('smart-plugs.setup.remove')}
              </Button>
            </div>
          </div>
        {/each}

        <div>
          <Button
            variant="secondary"
            size="sm"
            disabled={rows.length >= MAX_PLUGS}
            onclick={add}
          >
            {t('smart-plugs.setup.add')}
          </Button>
          {#if rows.length >= MAX_PLUGS}
            <span class="ms-3 text-xs text-muted-foreground"
              >{t('smart-plugs.setup.full')}</span
            >
          {/if}
        </div>

        <div class="grid gap-3 md:grid-cols-3">
          <div class="flex flex-col gap-1 md:col-span-2">
            <Label for="smart-plugs-price">{t('smart-plugs.setup.price')}</Label
            >
            <Input
              id="smart-plugs-price"
              value={price}
              oninput={(event: Event) => (price = typed(event))}
              inputmode="decimal"
              placeholder={t('smart-plugs.setup.price.placeholder')}
              aria-invalid={priceProblem !== undefined}
            />
            <p class="text-xs text-muted-foreground">
              {t('smart-plugs.setup.price.hint')}
            </p>
            {#if priceProblem}
              <p class="text-xs text-destructive-text">{priceProblem}</p>
            {/if}
          </div>
          <div class="flex flex-col gap-1">
            <Label for="smart-plugs-currency"
              >{t('smart-plugs.setup.currency')}</Label
            >
            <Input
              id="smart-plugs-currency"
              value={currency}
              oninput={(event: Event) => (currency = typed(event))}
              aria-invalid={currencyProblem !== undefined}
            />
            {#if currencyProblem}
              <p class="text-xs text-destructive-text">{currencyProblem}</p>
            {/if}
          </div>
        </div>
      </div>
    {/if}

    <div class="flex items-center justify-end gap-3">
      <Button variant="ghost" size="sm" onclick={onclose}
        >{t('smart-plugs.setup.cancel')}</Button
      >
      <Button
        size="sm"
        pending={saving}
        disabled={saving || loading || !resource || !valid}
        onclick={() => void save()}
      >
        {t('smart-plugs.setup.save')}
      </Button>
    </div>
  </CardContent>
</Card>
