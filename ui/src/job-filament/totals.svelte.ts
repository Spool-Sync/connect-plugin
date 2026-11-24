// The filament each print on the Jobs page took, read for the whole page at
// once: every cell asks for its print, and the asks of one tick go out as one
// request. A print is read once a visit.

import type { MaterialsService } from '../api';

/** At most this many prints a request, the most the route answers. */
const BATCH = 200;

export class JobTotals {
  /** The grams of each print read: a number, or null for a print with none counted. */
  #grams = $state<Record<string, number | null>>({});
  #waiting = new Set<string>();
  #asked = new Set<string>();
  #scheduled = false;
  readonly #service: MaterialsService;

  constructor(service: MaterialsService) {
    this.#service = service;
  }

  /**
   * The grams a print took: a number, null for a print this module counted
   * nothing for, and undefined while it is read.
   */
  of(jobId: string): number | null | undefined {
    if (!this.#asked.has(jobId)) {
      this.#asked.add(jobId);
      this.#waiting.add(jobId);
      this.#schedule();
    }

    return this.#grams[jobId];
  }

  #schedule(): void {
    if (this.#scheduled) return;
    this.#scheduled = true;
    queueMicrotask(() => {
      this.#scheduled = false;
      const ids = [...this.#waiting];
      this.#waiting.clear();
      for (let start = 0; start < ids.length; start += BATCH) {
        void this.#read(ids.slice(start, start + BATCH));
      }
    });
  }

  async #read(ids: readonly string[]): Promise<void> {
    try {
      const totals = await this.#service.jobsUsage(ids);
      const next: Record<string, number | null> = { ...this.#grams };
      for (const id of ids) next[id] = null;
      for (const total of totals) next[total.job_id] = total.grams;
      this.#grams = next;
    } catch {
      // A read that fails is asked again on the next visit; the cells stay empty.
      for (const id of ids) this.#asked.delete(id);
    }
  }
}

let held: JobTotals | undefined;

/** The one store every cell of the page shares. */
export const jobTotals = (service: MaterialsService): JobTotals =>
  (held ??= new JobTotals(service));
