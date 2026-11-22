// The module's typed routes, bound onto the client the host handed this bundle.
//
// The operations are Materials' route set, generated from Materials' OpenAPI
// document: the same paths, bodies and answers, which the SpoolSync module
// serves too. Their paths name Materials' mount, so the transport is wrapped
// once here to send each call to this module's own mount, which the host
// derives from the address the module is installed at.

import type { Transport } from '@prusa/connect';

import { OWN_MODULE } from '../own-module';
import { bindOperations, type Operations } from './generated/operations.js';

/** The mount the generated paths were written against. */
const GENERATED_MOUNT = '/api/v1/modules/com.github.aux.prusa3d.materials';

/** What bind takes from a client: the transport that sends its requests. */
export interface Client {
  readonly transport: Transport;
}

/** The transport, with each generated path moved to this module's mount. */
const mountedOn = (transport: Transport, mount: string): Transport => ({
  call: (operation, request, signal) =>
    transport.call(
      operation.path.startsWith(GENERATED_MOUNT)
        ? { ...operation, path: mount + operation.path.slice(GENERATED_MOUNT.length) }
        : operation,
      request,
      signal,
    ),
});

/** Attaches every route to the client's transport, under this module's mount. */
export function bind(client: Client): Operations {
  return bindOperations(mountedOn(client.transport, `/api/v1/modules/${OWN_MODULE}`));
}

export { bindOperations, operationTable, type Operations } from './generated/operations.js';
export type { components, operations, paths } from './generated/module-api.js';
