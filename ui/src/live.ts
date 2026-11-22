// What this module tells of its own changes, read back off the host's event
// stream so a page follows a print as it happens: a tool whose material or
// spool changed, and a spool that changed, such as one losing filament. The
// module publishes them with bus.emit, and the host names each type after the
// namespace it installed the module under.
//
// The SDK's subscription reconnects by itself and resumes where it left off.
// A place in the stream the core could not keep is told to every endpoint at
// once, with no payload, since anything may have changed while it was lost.
import type { PrusaConnect } from '@prusa/connect';

import { OWN_MODULE } from './own-module';

export type Endpoint = 'tool' | 'spool';

/** What a tool event carries. */
export interface ToolChange {
  device_id: string;
  tool_id: string;
  material_uuid?: string;
  spool_id?: number;
}

/** What a spool event carries. */
export interface SpoolChange {
  spool_id: number;
  spool_uuid: string;
  material_uuid: string;
  remaining_weight_g: number;
  deleted?: boolean;
}

const CONTINUITY_LOST = 'com.prusa.connect.stream.continuity.lost';

/**
 * Calls `on` for each change this module tells of on the endpoints, until
 * `signal` aborts. A payload is undefined when the stream lost its place.
 */
export const follow = (
  client: PrusaConnect,
  signal: AbortSignal,
  endpoints: readonly Endpoint[],
  on: (
    endpoint: Endpoint,
    change: ToolChange | SpoolChange | undefined,
  ) => void,
): void => {
  const typeOf = new Map(endpoints.map((e) => [`${OWN_MODULE}.${e}`, e]));
  // The module's types are its own, and the SDK knows only the core's.
  const types = [...typeOf.keys(), CONTINUITY_LOST] as never[];

  void (async () => {
    try {
      for await (const event of client.events({ types, signal })) {
        const endpoint = typeOf.get(event.type);
        if (endpoint !== undefined) {
          on(endpoint, event.payload as ToolChange | SpoolChange);
        } else {
          for (const e of endpoints) on(e, undefined);
        }
      }
    } catch {
      // The subscription reconnects by itself. One that throws anyway leaves
      // the page to its own reads.
    }
  })();
};
