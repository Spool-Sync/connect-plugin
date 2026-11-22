// An error, in the words the page shows (frontend-i18n.md rule 5).
//
// `describeFault` reads the thrown value by shape and answers the code, a
// title and the server's English detail. It looks the title up in the core's
// catalogue, and this module's codes are in this module's own catalogue, so
// the line is looked up again here through the `t` the host bound to this
// module's namespace.
//
// The page keeps the English the server sent under the translated line. It
// names the field and the limit, which a catalogue line cannot, and a person
// reading a Czech screen can paste it into a bug report.
import { describeFault, type FaultText } from '@prusa/connect-ui/i18n';
import type { Translate } from '@prusa/connect-ui/i18n';

/**
 * The three parts of an error: its code, the line to show, and the English
 * detail under it.
 *
 * A key no catalogue holds reads back as the key itself, which is how this
 * tells a code the module has a line for from one it does not. A code with no
 * line keeps the server's English as the title, which is what rule 5 asks for.
 */
export const faultText = (thrown: unknown, t: Translate): FaultText => {
  const fault = describeFault(thrown);
  if (fault.code === undefined) return fault;

  const key = `error.${fault.code}`;
  const line = t(key);
  if (line === key) return fault;

  return { code: fault.code, title: line, detail: fault.detail ?? fault.title };
};
