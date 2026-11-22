// The spools the add dialog is about to make, as the person types them.
//
// A class, because both steps of the dialog read and write it and step 2's
// summary derives from it (svelte-5.md). It knows nothing of the network:
// `body()` is what the dialog posts.
import type { Detail, SpoolsDraft } from '../api';

/** A weight the person typed: a number, and not negative. */
const weight = (grams: number | undefined): boolean =>
  grams === undefined || (Number.isFinite(grams) && grams >= 0);

export class SpoolDraftForm {
  step = $state<1 | 2>(1);
  material = $state.raw<Detail | undefined>(undefined);
  quantity = $state(1);
  packageUuid = $state<string | undefined>(undefined);
  fullWeightG = $state<number | undefined>(undefined);
  emptyWeightG = $state<number | undefined>(undefined);
  lengthMM = $state<number | undefined>(undefined);
  /** What is left on each spool. Left empty, the spools are full and read `netG`. */
  remainingG = $state<number | undefined>(undefined);
  /** The location the spools are kept at, none when undefined. */
  locationId = $state<string | undefined>(undefined);
  notes = $state('');

  /**
   * Takes the material and moves on to step 2. Another material drops the
   * package and the weights it filled, which belonged to the old one.
   */
  choose(detail: Detail): void {
    if (this.material?.uuid !== detail.uuid) {
      this.packageUuid = undefined;
      this.fullWeightG = undefined;
      this.emptyWeightG = undefined;
      this.lengthMM = undefined;
    }
    this.material = detail;
    this.step = 2;
  }

  /**
   * Takes a catalogue package and fills the weights and the length from it.
   * The full weight is the filament and the empty spool together. A field the
   * package does not know is left for the person to type. No package keeps
   * what was typed.
   */
  choosePackage(uuid?: string): void {
    this.packageUuid = uuid;
    const chosen = this.material?.packages.find((p) => p.uuid === uuid);
    if (chosen === undefined) return;

    const { net_weight_g: net, empty_weight_g: empty, length_mm } = chosen;
    if (empty !== undefined) this.emptyWeightG = empty;
    if (net !== undefined && empty !== undefined)
      this.fullWeightG = net + empty;
    if (length_mm !== undefined) this.lengthMM = length_mm;
  }

  /** The filament on a full spool, when both weights are known. */
  get netG(): number | undefined {
    if (this.fullWeightG === undefined || this.emptyWeightG === undefined) {
      return undefined;
    }

    return this.fullWeightG - this.emptyWeightG;
  }

  /** What each spool will have left: all of it unless typed. */
  get remaining(): number | undefined {
    return this.remainingG ?? this.netG;
  }

  /** What the module would accept, checked before it is asked. */
  get valid(): boolean {
    const left = this.remaining;
    const net = this.netG;

    return (
      this.material !== undefined &&
      Number.isInteger(this.quantity) &&
      this.quantity >= 1 &&
      this.quantity <= 100 &&
      left !== undefined &&
      weight(left) &&
      weight(this.fullWeightG) &&
      weight(this.emptyWeightG) &&
      (net === undefined || (net >= 0 && left <= net))
    );
  }

  /** The request body. Call it only when `valid` holds. */
  body(): SpoolsDraft {
    const body: SpoolsDraft = {
      material_uuid: this.material?.uuid ?? '',
      quantity: this.quantity,
      remaining_weight_g: this.remaining ?? 0,
      notes: this.notes.trim(),
    };
    if (this.fullWeightG !== undefined) body.full_weight_g = this.fullWeightG;
    if (this.emptyWeightG !== undefined)
      body.empty_weight_g = this.emptyWeightG;
    if (this.locationId !== undefined) body.location_id = this.locationId;

    return body;
  }
}
