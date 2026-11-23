// Which spools a tool takes, by the material the printer reports on it. The
// same rule as the module's own (internal/assign/rules.go): a name is the
// same in any case, an alias group counts as one name, and a name that is no
// known type is one a person gave the filament, so it narrows nothing.

/** Groups of materials a printer reports by one name: PLA for WOOD, PC for PCCF. */
const ALIASES: readonly (readonly string[])[] = [
  ['PC', 'PCCF'],
  ['PLA', 'WOOD'],
  ['FLEX', 'TPU', 'TPE'],
];

/** The FFF material types a printer reports, with the names in ALIASES. */
const KNOWN = new Set(
  (
    'PLA PETG TPU ABS ASA PC PCTG PP PA6 PA11 PA12 PA612 PA66 CPE TPE HIPS ' +
    'PHA PET PEI PBT PVB PVA PEKK PEEK BVOH TPC PPS PPSU PVC PEBA PVDF PPA ' +
    'PCL PES PMMA POM PPE PS PSU TPI SBS OBC EVA PCCF WOOD FLEX'
  ).split(' '),
);

/**
 * The material a tool's spools must be, or undefined when the printer reports
 * none, or a name of its own that any spool may stand for.
 */
export const requiredMaterial = (reported: string | undefined): string | undefined => {
  const name = reported?.trim() ?? '';

  return name !== '' && KNOWN.has(name.toUpperCase()) ? name : undefined;
};

/** Whether a spool of `type` is the material the printer reports. */
export const sameMaterial = (reported: string, type: string): boolean => {
  const a = reported.trim().toUpperCase();
  const b = type.trim().toUpperCase();
  if (b === '') return false;
  if (a === b) return true;

  return ALIASES.some((group) => group.includes(a) && group.includes(b));
};
