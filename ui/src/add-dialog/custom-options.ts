// The vocabularies the custom material form offers: the presets and the colour
// palette. Only what SpoolSync's filament type holds is here: a preset gives a
// filament's density, and the palette gives the primary colour.

/** The density a preset fills in, in g/cm³. Every one stays editable. */
export interface PresetValues {
  density?: number;
}

/** Starting points for a filament's density, as the app offers them. */
export const PRESETS: readonly { type: string; values: PresetValues }[] = [
  { type: 'PLA', values: { density: 1.24 } },
  { type: 'PETG', values: { density: 1.27 } },
  { type: 'TPU', values: { density: 1.21 } },
  { type: 'ABS', values: { density: 1.04 } },
  { type: 'ASA', values: { density: 1.07 } },
  { type: 'PC', values: { density: 1.2 } },
  { type: 'PET', values: { density: 1.38 } },
  { type: 'PVB', values: { density: 1.1 } },
];

/** The primary colours the palette offers, as `#rrggbb`. */
export const PALETTE: readonly string[] = [
  '#E02020',
  '#F08000',
  '#F4D03F',
  '#2ECC71',
  '#17A2B8',
  '#2762ED',
  '#8E44AD',
  '#B8651E',
  '#111111',
  '#F5F5F5',
  '#808080',
];
