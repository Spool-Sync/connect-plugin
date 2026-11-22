// The vocabularies the custom material form offers: the presets, the colour
// palette, and the OpenPrintTag tags and certifications.

import type { MaterialFields } from '../api';

/** The numbers a preset fills in; every one stays editable. */
export type PresetValues = Partial<
  Pick<
    MaterialFields,
    | 'density'
    | 'min_print_temp'
    | 'max_print_temp'
    | 'preheat_temp'
    | 'min_bed_temp'
    | 'max_bed_temp'
    | 'chamber_temp'
    | 'min_chamber_temp'
    | 'max_chamber_temp'
    | 'drying_temp'
    | 'drying_time_min'
    | 'min_nozzle_diameter'
  >
>;

/** Starting points for a filament's temperatures and density, as the app offers them. */
export const PRESETS: readonly { type: string; values: PresetValues }[] = [
  {
    type: 'PLA',
    values: {
      density: 1.24,
      min_print_temp: 180,
      max_print_temp: 220,
      preheat_temp: 160,
      max_bed_temp: 60,
      chamber_temp: 25,
      min_chamber_temp: 15,
      max_chamber_temp: 35,
      drying_temp: 45,
      drying_time_min: 240,
    },
  },
  {
    type: 'PETG',
    values: {
      density: 1.27,
      min_print_temp: 220,
      max_print_temp: 250,
      preheat_temp: 170,
      min_bed_temp: 60,
      max_bed_temp: 80,
      chamber_temp: 30,
      min_chamber_temp: 15,
      max_chamber_temp: 50,
      drying_temp: 65,
      drying_time_min: 360,
    },
  },
  {
    type: 'TPU',
    values: {
      density: 1.21,
      min_print_temp: 210,
      max_print_temp: 240,
      preheat_temp: 160,
      min_bed_temp: 20,
      max_bed_temp: 60,
      chamber_temp: 25,
      min_chamber_temp: 15,
      max_chamber_temp: 40,
      drying_temp: 60,
      drying_time_min: 360,
      min_nozzle_diameter: 0.4,
    },
  },
  {
    type: 'ABS',
    values: {
      density: 1.04,
      min_print_temp: 230,
      max_print_temp: 260,
      preheat_temp: 180,
      min_bed_temp: 90,
      max_bed_temp: 110,
      chamber_temp: 50,
      min_chamber_temp: 40,
      max_chamber_temp: 80,
      drying_temp: 80,
      drying_time_min: 240,
    },
  },
  {
    type: 'ASA',
    values: {
      density: 1.07,
      min_print_temp: 230,
      max_print_temp: 260,
      preheat_temp: 180,
      min_bed_temp: 90,
      max_bed_temp: 110,
      chamber_temp: 50,
      min_chamber_temp: 40,
      max_chamber_temp: 80,
      drying_temp: 80,
      drying_time_min: 240,
    },
  },
  {
    type: 'PC',
    values: {
      density: 1.2,
      min_print_temp: 270,
      max_print_temp: 310,
      preheat_temp: 200,
      min_bed_temp: 110,
      max_bed_temp: 130,
      chamber_temp: 80,
      min_chamber_temp: 60,
      max_chamber_temp: 100,
      drying_temp: 120,
      drying_time_min: 480,
    },
  },
  {
    type: 'PET',
    values: {
      density: 1.38,
      min_print_temp: 255,
      max_print_temp: 280,
      preheat_temp: 190,
      min_bed_temp: 70,
      max_bed_temp: 90,
      chamber_temp: 40,
      min_chamber_temp: 20,
      max_chamber_temp: 60,
      drying_temp: 65,
      drying_time_min: 360,
    },
  },
  {
    type: 'PVB',
    values: {
      density: 1.1,
      min_print_temp: 200,
      max_print_temp: 225,
      preheat_temp: 160,
      min_bed_temp: 60,
      max_bed_temp: 75,
      chamber_temp: 25,
      min_chamber_temp: 15,
      max_chamber_temp: 45,
      drying_temp: 55,
      drying_time_min: 360,
    },
  },
];

/** The primary colours offered as swatches; any other comes from the picker. */
export const PALETTE = [
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
] as const;

/** The most secondary colours one material lists. */
export const MAX_SECONDARY = 5;

/** A certification as it travels, and its name, the same in every language. */
export interface Label {
  name: string;
  label: string;
}

/**
 * The OpenPrintTag tags, by name. Each reads as `materials.tag.<name>` in the
 * module's catalogue.
 */
export const TAGS: readonly string[] = [
  'filtration_recommended',
  'biocompatible',
  'home_compostable',
  'industrially_compostable',
  'bio_based',
  'antibacterial',
  'air_filtering',
  'abrasive',
  'foaming',
  'castable',
  'self_extinguishing',
  'paramagnetic',
  'radiation_shielding',
  'high_temperature',
  'high_speed',
  'esd_safe',
  'conductive',
  'emi_shielding',
  'blend',
  'water_soluble',
  'ipa_soluble',
  'limonene_soluble',
  'low_outgassing',
  'matte',
  'silk',
  'translucent',
  'transparent',
  'without_pigments',
  'iridescent',
  'pearlescent',
  'glitter',
  'glow_in_the_dark',
  'neon',
  'illuminescent_color_change',
  'temperature_color_change',
  'gradual_color_change',
  'coextruded',
  'contains_carbon',
  'contains_carbon_fiber',
  'contains_carbon_nano_tubes',
  'contains_graphene',
  'contains_glass',
  'contains_glass_fiber',
  'contains_kevlar',
  'contains_ptfe',
  'contains_stone',
  'contains_magnetite',
  'contains_organic_material',
  'contains_cork',
  'contains_wax',
  'contains_wood',
  'contains_algae',
  'contains_bamboo',
  'contains_pine',
  'contains_ceramic',
  'contains_boron_carbide',
  'contains_metal',
  'contains_bronze',
  'contains_iron',
  'contains_steel',
  'contains_silver',
  'contains_copper',
  'contains_aluminium',
  'contains_brass',
  'contains_tungsten',
  'imitates_wood',
  'imitates_metal',
  'imitates_marble',
  'imitates_stone',
  'lithophane',
  'recycled',
  'limited_edition',
];

export const CERTIFICATIONS: readonly Label[] = [
  { name: 'ul_2818', label: 'UL 2818' },
  { name: 'ul_94_v0', label: 'UL 94 V0' },
  { name: 'ul_2904', label: 'UL 2904' },
];
