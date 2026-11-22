// A printer as Core's own printer list draws it: its artwork, its model and
// its state with the tint the state wears. Core keeps the helpers behind its
// list private, so the two small mappings below follow
// packages/svelte/src/shell/printers/presentation.ts.
import {
  PRINTER_STATE_TONE,
  type PrinterState,
  type PrinterStateTone,
  printerStateOf,
} from '@prusa/connect-ui/domain';
import {
  type BadgeVariant,
  PRINTER_MODELS,
  type PrinterModel,
} from '@prusa/connect-ui/primitives';

const MODEL_BY_SPELLING: ReadonlyMap<string, PrinterModel> = new Map(
  PRINTER_MODELS.map((model) => [model.replaceAll('_', ''), model]),
);

/** The artwork a printer's own spelling of its model asks for. */
export const artworkOf = (model: string): PrinterModel =>
  MODEL_BY_SPELLING.get(model.toLowerCase().replace(/[^a-z0-9]/g, '')) ??
  'unknown';

/** A printer that is not connected reads OFFLINE, whatever it said last. */
export const stateOf = (connected: boolean, state: string): PrinterState =>
  connected ? printerStateOf(state === '' ? 'UNKNOWN' : state) : 'OFFLINE';

// One badge per tone the published library names. The checkout's library
// also tells busy, moving and ready apart; this one shows them as the tone.
const BADGE: Record<PrinterStateTone, BadgeVariant> = {
  active: 'stateActive',
  done: 'stateDone',
  idle: 'stateIdle',
  attention: 'stateAttention',
  fault: 'stateFault',
  quiet: 'stateQuiet',
};

/** The badge a state wears. */
export const stateBadge = (state: PrinterState): BadgeVariant =>
  BADGE[PRINTER_STATE_TONE[state]];
