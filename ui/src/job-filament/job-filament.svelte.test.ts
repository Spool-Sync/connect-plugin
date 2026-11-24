import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import { stubHost } from '../test/host';
import JobCell from './job-cell.svelte';
import JobSection from './job-section.svelte';

const NS = 'life.honex.gitlab.aux.spoolsync.spool-sync-prusa-connect';
const M = `/api/v1/modules/${NS}`;

describe('the filament of a print', () => {
  it('reads every cell of a page in one request, and draws nothing for a print not counted', async () => {
    const stub = stubHost([{ status: 200, body: {} }], {
      routed: {
        [`${M}/jobs/usage`]: [
          { status: 200, body: { usage: [{ job_id: 'a', grams: 38.4 }] } },
        ],
      },
    });
    void setup(stub.host);

    // The Jobs page mounts every cell of its page in one tick.
    await Promise.all([
      render(JobCell, { jobId: 'a' }),
      render(JobCell, { jobId: 'b' }),
    ]);

    await expect.element(page.getByText('38 g')).toBeVisible();
    const reads = stub.calls.filter((c) => c.url.includes('/jobs/usage'));
    expect(reads).toHaveLength(1);
    // The client joins the ids with commas, which the route reads.
    expect(decodeURIComponent(reads[0]?.url ?? '')).toContain('id=a,b');
    expect(document.querySelectorAll('[data-part="job-grams"]')).toHaveLength(
      1,
    );
  });

  it('lists the spools a print took filament off, with the total', async () => {
    const stub = stubHost([{ status: 200, body: {} }], {
      routed: {
        [`${M}/jobs/j1/usage`]: [
          {
            status: 200,
            body: {
              job_id: 'j1',
              grams: 54.5,
              spools: [
                {
                  tool: '0',
                  spool_id: 's1',
                  name: 'Galaxy Black',
                  brand: 'Prusament',
                  material: 'PLA',
                  color_rgba: '#3d3e3dff',
                  grams: 42,
                },
                {
                  tool: '1',
                  spool_id: 's2',
                  name: '',
                  brand: '',
                  material: '',
                  color_rgba: '',
                  grams: 12.5,
                },
              ],
            },
          },
        ],
      },
    });
    void setup(stub.host);

    await render(JobSection, { jobId: 'j1' });

    await expect
      .element(page.getByText('Prusament Galaxy Black'))
      .toBeVisible();
    await expect.element(page.getByText('Tool 1 · PLA')).toBeVisible();
    await expect.element(page.getByText('A spool since deleted')).toBeVisible();
    await expect.element(page.getByText('55 g')).toBeVisible();
  });

  it('says when nothing was counted for a print', async () => {
    const stub = stubHost([{ status: 200, body: {} }], {
      routed: {
        [`${M}/jobs/j2/usage`]: [
          { status: 200, body: { job_id: 'j2', grams: 0, spools: [] } },
        ],
      },
    });
    void setup(stub.host);

    await render(JobSection, { jobId: 'j2' });

    await expect
      .element(
        page.getByText('No filament was counted for this print', {
          exact: false,
        }),
      )
      .toBeVisible();
  });
});
