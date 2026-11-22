import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  materialsOn,
  type Catalogue as Status,
  type MaterialsService,
} from './api';
import { Catalogue } from './catalogue.svelte';
import { stubHost } from './test/host';

const MOUNT = '/api/v1/modules/life.honex.gitlab.aux.spoolsync.spool-sync-prusa-connect';

const status = (phase: 'idle' | 'running' | 'failed') => ({
  loaded: phase === 'idle',
  import: { phase },
});

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe('the catalogue', () => {
  it('stays busy until the last of two overlapping calls answers', async () => {
    let answerRead: (answer: Status) => void = () => undefined;
    let answerDownload: (answer: Status) => void = () => undefined;
    const service = {
      catalogue: () => new Promise<Status>((resolve) => (answerRead = resolve)),
      download: () =>
        new Promise<Status>((resolve) => (answerDownload = resolve)),
    } as unknown as MaterialsService;
    const catalogue = new Catalogue(service);

    const read = catalogue.read();
    const download = catalogue.download();
    answerRead(status('idle'));
    await read;
    expect(catalogue.busy).toBe(true);

    answerDownload(status('running'));
    await download;
    expect(catalogue.busy).toBe(false);
  });

  it('starts the download and keeps the status the module answered', async () => {
    const stub = stubHost([{ status: 202, body: status('running') }]);
    const catalogue = new Catalogue(materialsOn(stub.host));

    expect(await catalogue.download()).toBe(true);

    expect(stub.calls[0]).toMatchObject({
      method: 'POST',
      url: `${MOUNT}/catalogue/download`,
    });
    expect(catalogue.status?.import.phase).toBe('running');
  });

  it('reads the import somebody else started when the download is refused as running', async () => {
    const stub = stubHost([
      {
        status: 409,
        contentType: 'application/problem+json',
        body: {
          code: 'materials/IMPORT_RUNNING',
          status: 409,
          title: 'running',
          detail: 'running',
        },
      },
      { status: 200, body: status('running') },
    ]);
    const catalogue = new Catalogue(materialsOn(stub.host));

    expect(await catalogue.download()).toBe(true);

    expect(catalogue.fault).toBeUndefined();
    expect(catalogue.status?.import.phase).toBe('running');
  });

  it('polls every 2 s and stops once the phase leaves running', async () => {
    const stub = stubHost([
      { status: 200, body: status('running') },
      { status: 200, body: status('running') },
      { status: 200, body: status('idle') },
    ]);
    const catalogue = new Catalogue(materialsOn(stub.host));
    await catalogue.read();

    catalogue.poll(new AbortController().signal);
    await vi.advanceTimersByTimeAsync(1999);
    expect(stub.calls).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(stub.calls).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(2000);
    expect(stub.calls).toHaveLength(3);
    await vi.waitFor(() => {
      expect(catalogue.status?.import.phase).toBe('idle');
    });

    await vi.advanceTimersByTimeAsync(10_000);
    expect(stub.calls).toHaveLength(3);
  });

  it('stops polling when the signal aborts', async () => {
    const stub = stubHost([{ status: 200, body: status('running') }]);
    const catalogue = new Catalogue(materialsOn(stub.host));
    await catalogue.read();
    const ended = new AbortController();

    catalogue.poll(ended.signal);
    await vi.advanceTimersByTimeAsync(2000);
    expect(stub.calls).toHaveLength(2);
    ended.abort();
    await vi.advanceTimersByTimeAsync(10_000);

    expect(stub.calls).toHaveLength(2);
  });

  it('runs one poll loop however often it is started, and leaves nothing behind', async () => {
    const stub = stubHost([
      { status: 200, body: status('running') },
      { status: 200, body: status('idle') },
    ]);
    const catalogue = new Catalogue(materialsOn(stub.host));
    await catalogue.read();
    const signal = new AbortController().signal;
    const added = vi.spyOn(signal, 'addEventListener');
    const removed = vi.spyOn(signal, 'removeEventListener');

    catalogue.poll(signal);
    catalogue.poll(signal);
    expect(vi.getTimerCount()).toBe(1);
    await vi.advanceTimersByTimeAsync(2000);
    expect(stub.calls).toHaveLength(2);
    await vi.waitFor(() => {
      expect(catalogue.status?.import.phase).toBe('idle');
    });

    expect(vi.getTimerCount()).toBe(0);
    expect(added).toHaveBeenCalledTimes(removed.mock.calls.length);
  });

  it('polls on a new signal after the old one aborted during a read', async () => {
    let calls = 0;
    let release: (status: Status) => void = () => undefined;
    const service: MaterialsService = {
      brands: () => Promise.resolve([]),
      types: () => Promise.resolve([]),
      locations: () => Promise.resolve([]),
      createLocation: () => Promise.reject(new Error('unused')),
      renameLocation: () => Promise.reject(new Error('unused')),
      deleteLocation: () => Promise.resolve(),
      filters: () => Promise.resolve({}),
      keepFilters: () => Promise.resolve(),
      material: () => Promise.reject(new Error('unused')),
      createCustom: () => Promise.reject(new Error('unused')),
      addSpools: () => Promise.reject(new Error('unused')),
      spools: () => Promise.reject(new Error('unused')),
      updateSpool: () => Promise.reject(new Error('unused')),
      deleteSpool: () => Promise.reject(new Error('unused')),
      deleteSpools: () => Promise.reject(new Error('unused')),
      updateCustom: () => Promise.reject(new Error('unused')),
      deleteCustom: () => Promise.reject(new Error('unused')),
      tools: () => Promise.reject(new Error('unused')),
      assign: () => Promise.reject(new Error('unused')),
      unassign: () => Promise.reject(new Error('unused')),
      addReported: () => Promise.reject(new Error('unused')),
      printers: () => Promise.resolve([]),
      printerPage: () => Promise.resolve({ printers: [] }),
      merge: () => Promise.reject(new Error('unused')),
      list: () => Promise.reject(new Error('unused')),
      star: () => Promise.resolve(),
      catalogue: () => {
        calls += 1;
        if (calls === 1) return Promise.resolve(status('running'));

        return new Promise<Status>((resolve) => {
          release = resolve;
        });
      },
      download: () => Promise.reject(new Error('unused')),
    };
    const catalogue = new Catalogue(service);
    await catalogue.read();
    const old = new AbortController();

    catalogue.poll(old.signal);
    await vi.advanceTimersByTimeAsync(2000);
    expect(calls).toBe(2);
    old.abort();
    catalogue.poll(new AbortController().signal);
    release(status('running'));
    await vi.advanceTimersByTimeAsync(2000);

    expect(calls).toBe(3);
  });

  it('keeps a download answer over a read that was asked before it', async () => {
    let releaseRead: (status: Status) => void = () => undefined;
    const service: MaterialsService = {
      brands: () => Promise.resolve([]),
      types: () => Promise.resolve([]),
      locations: () => Promise.resolve([]),
      createLocation: () => Promise.reject(new Error('unused')),
      renameLocation: () => Promise.reject(new Error('unused')),
      deleteLocation: () => Promise.resolve(),
      filters: () => Promise.resolve({}),
      keepFilters: () => Promise.resolve(),
      material: () => Promise.reject(new Error('unused')),
      createCustom: () => Promise.reject(new Error('unused')),
      addSpools: () => Promise.reject(new Error('unused')),
      spools: () => Promise.reject(new Error('unused')),
      updateSpool: () => Promise.reject(new Error('unused')),
      deleteSpool: () => Promise.reject(new Error('unused')),
      deleteSpools: () => Promise.reject(new Error('unused')),
      updateCustom: () => Promise.reject(new Error('unused')),
      deleteCustom: () => Promise.reject(new Error('unused')),
      tools: () => Promise.reject(new Error('unused')),
      assign: () => Promise.reject(new Error('unused')),
      unassign: () => Promise.reject(new Error('unused')),
      addReported: () => Promise.reject(new Error('unused')),
      printers: () => Promise.resolve([]),
      printerPage: () => Promise.resolve({ printers: [] }),
      merge: () => Promise.reject(new Error('unused')),
      list: () => Promise.reject(new Error('unused')),
      star: () => Promise.resolve(),
      catalogue: () =>
        new Promise<Status>((resolve) => {
          releaseRead = resolve;
        }),
      download: () => Promise.resolve(status('running')),
    };
    const catalogue = new Catalogue(service);

    const read = catalogue.read();
    await catalogue.download();
    releaseRead(status('idle'));
    await read;

    expect(catalogue.status?.import.phase).toBe('running');
  });
});
