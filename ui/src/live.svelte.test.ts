import { describe, expect, it, vi } from 'vitest';

import type { PrusaConnect } from '@prusa/connect';

import { follow } from './live';
import { OWN_MODULE } from './own-module';

const CONTINUITY_LOST = 'com.prusa.connect.stream.continuity.lost';

/** A client whose stream yields events, and records what it was asked for. */
const streaming = (events: { type: string; payload?: unknown }[]) => {
  const asked: { types?: readonly string[] }[] = [];
  const client = {
    events: (options: { types?: readonly string[] }) => {
      asked.push(options);

      // `for await` reads a plain list as it reads a stream.
      return events;
    },
  } as unknown as PrusaConnect;

  return { client, asked };
};

describe('following the module’s own changes', () => {
  it('asks for its own types and tells each change by its endpoint', async () => {
    const spool = {
      spool_id: 8,
      material_uuid: 'm-1',
      remaining_weight_g: 499,
    };
    const { client, asked } = streaming([
      { type: `${OWN_MODULE}.spool`, payload: spool },
    ]);
    const on = vi.fn();

    follow(client, new AbortController().signal, ['tool', 'spool'], on);

    await vi.waitFor(() => {
      expect(on).toHaveBeenCalledWith('spool', spool);
    });
    expect(asked[0]?.types).toEqual([
      `${OWN_MODULE}.tool`,
      `${OWN_MODULE}.spool`,
      CONTINUITY_LOST,
    ]);
  });

  it('tells every endpoint of a lost place in the stream', async () => {
    const { client } = streaming([{ type: CONTINUITY_LOST }]);
    const on = vi.fn();

    follow(client, new AbortController().signal, ['tool', 'spool'], on);

    await vi.waitFor(() => {
      expect(on.mock.calls).toEqual([
        ['tool', undefined],
        ['spool', undefined],
      ]);
    });
  });
});
