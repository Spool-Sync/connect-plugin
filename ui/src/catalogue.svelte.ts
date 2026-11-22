// The catalogue as the pages see it: whether one is loaded, and how the
// import of the next one is going.
import { describeFault } from '@prusa/connect-ui/i18n';

import type { Catalogue as Status, MaterialsService } from './api';

/** How often the status is read again while an import runs. */
const POLL_MS = 2000;

/** Resolves after `ms`, or at once when the signal aborts. */
const wait = (ms: number, signal: AbortSignal): Promise<void> =>
  new Promise((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const end = (): void => {
      clearTimeout(timer);
      signal.removeEventListener('abort', end);
      resolve();
    };
    const timer = setTimeout(end, ms);
    signal.addEventListener('abort', end, { once: true });
  });

export class Catalogue {
  readonly #service: MaterialsService;
  #status = $state.raw<Status | undefined>(undefined);
  /** The calls in flight; a read and a download may overlap. */
  #running = $state(0);
  #fault = $state.raw<unknown>(undefined);
  #flight: Promise<void> | undefined;
  /** Bumped by every call that may set the status, so the newest answer wins. */
  #seq = 0;
  #flightSeq = 0;
  /** The signal of the poll loop that runs, and undefined when none does. */
  #pollSignal: AbortSignal | undefined;

  constructor(service: MaterialsService) {
    this.#service = service;
  }

  /** What the module answered last, and undefined until it answered. */
  get status(): Status | undefined {
    return this.#status;
  }

  /** False until the first read answered, which is not the same as no catalogue. */
  get loaded(): boolean {
    return this.#status !== undefined;
  }

  /** A call is in flight. */
  get busy(): boolean {
    return this.#running > 0;
  }

  /** What the last call threw, and undefined when none did. */
  get fault(): unknown {
    return this.#fault;
  }

  /** Reads the status. Two calls at once make one request. */
  read(): Promise<void> {
    if (this.#flight !== undefined && this.#flightSeq === this.#seq) {
      return this.#flight;
    }
    const mine = (this.#seq += 1);
    this.#flightSeq = mine;
    const flight = this.#run(async () => {
      const status = await this.#service.catalogue();
      // A download that went out after this read has the newer answer.
      if (mine === this.#seq) this.#status = status;
    }).finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  /**
   * Starts the download and reports whether an import now runs. One somebody
   * else started is as good as this one: the status is read, so the caller
   * polls it to the end.
   */
  async download(): Promise<boolean> {
    const mine = (this.#seq += 1);

    return await this.#run(async () => {
      let status: Status;
      try {
        status = await this.#service.download();
      } catch (thrown) {
        if (describeFault(thrown).code !== 'materials/IMPORT_RUNNING') {
          throw thrown;
        }
        status = await this.#service.catalogue();
      }
      if (mine === this.#seq) this.#status = status;
    }).then(() => this.#fault === undefined);
  }

  /**
   * Reads the status every 2 s for as long as an import runs. It ends when the
   * phase leaves `running` and when the signal aborts, and it does nothing
   * when no import runs or one loop already runs.
   */
  poll(signal: AbortSignal): void {
    // A loop whose signal fired is on its way out, so it does not hold this one off.
    if (this.#pollSignal !== undefined && !this.#pollSignal.aborted) return;
    if (signal.aborted || this.#status?.import.phase !== 'running') return;
    this.#pollSignal = signal;
    const gone = (): boolean => signal.aborted;

    void (async () => {
      try {
        do {
          await wait(POLL_MS, signal);
          if (gone()) break;
          await this.read();
          if (gone()) break;
        } while (this.#status?.import.phase === 'running');
      } finally {
        if (this.#pollSignal === signal) this.#pollSignal = undefined;
      }
    })();
  }

  async #run(call: () => Promise<void>): Promise<void> {
    this.#running += 1;
    this.#fault = undefined;

    try {
      await call();
    } catch (thrown) {
      this.#fault = thrown;
    } finally {
      this.#running -= 1;
    }
  }
}

let held: Catalogue | undefined;

/** Called once by the module's setup function, with the host's signal. */
export const holdCatalogue = (
  catalogue: Catalogue,
  signal: AbortSignal,
): void => {
  if (signal.aborted) return;
  held = catalogue;
  signal.addEventListener(
    'abort',
    () => {
      if (held === catalogue) held = undefined;
    },
    { once: true },
  );
};

/** The catalogue the pages read. It throws rather than drawing an empty screen. */
export const heldCatalogue = (): Catalogue => {
  if (!held) {
    throw new Error('the materials module drew a page before its setup ran');
  }

  return held;
};
