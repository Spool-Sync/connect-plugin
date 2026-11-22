// Why a change to a tool is refused.
//
// The module answers a reason for each tool, and the same reason in `params`
// when it refuses a change. Busy covers a filament change, and also a state
// the rules do not trust, such as ATTENTION. That one names the state
// instead, as Core's printer list labels it.
import { PRINTER_STATE_LABEL_KEY } from '@prusa/connect-ui/domain';
import { t as coreT, type Translate } from '@prusa/connect-ui/i18n';

import { stateOf } from '../printer-row';

/** The busy states: the printer is changing filament. */
const CHANGING = new Set(['BUSY', 'MANIPULATING']);

/** The text for a reason, and undefined when the change is allowed. */
export const reasonText = (
  t: Translate,
  reason: string,
  action: 'assign' | 'unassign',
  state: string,
  reported: string,
): string | undefined => {
  switch (reason) {
    case 'printing':
      return action === 'assign'
        ? t('materials.printer.assignPrinting')
        : t('materials.printer.unassignPrinting');
    case 'busy':
      return CHANGING.has(state)
        ? t('materials.printer.busy')
        : t('materials.printer.state', {
            state: coreT(PRINTER_STATE_LABEL_KEY[stateOf(true, state)]),
          });
    case 'reported-empty':
      return t('materials.printer.reportedEmpty');
    case 'reported-loaded':
      return t('materials.printer.mismatch', { reported });
    case 'nfc':
      return t('materials.printer.nfc');
    default:
      return undefined;
  }
};

/**
 * The text for a TOOL_REFUSED the module answered, and undefined for any
 * other fault, which the page draws by its code.
 */
export const refusalText = (
  t: Translate,
  thrown: unknown,
  action: 'assign' | 'unassign',
  reported: string,
): string | undefined => {
  if (typeof thrown !== 'object' || thrown === null) return undefined;
  const { code, params } = thrown as {
    code?: unknown;
    params?: { reason?: unknown; state?: unknown };
  };
  if (code !== 'materials/TOOL_REFUSED') return undefined;
  const text = (value: unknown): string =>
    typeof value === 'string' ? value : '';

  return reasonText(
    t,
    text(params?.reason),
    action,
    text(params?.state),
    reported,
  );
};
