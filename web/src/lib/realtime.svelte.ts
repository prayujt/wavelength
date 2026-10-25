import { Code, ConnectError } from '@connectrpc/connect';
import type { ImportJob } from '../gen/wavelength/v1/ingestion_pb';
import type { DownloadSelection } from './downloads.svelte';
import { api, session } from './session.svelte';

export class RealtimeState {
  connected = $state(false);
  resetVersion = $state(0);
  libraryVersion = $state(0);
  imports = $state<Record<string, ImportJob>>({});

  latest(job: ImportJob) {
    const update = this.imports[job.id];
    if (!update) return job;
    if (job.updatedAt && update.updatedAt) {
      if (
        job.updatedAt.seconds > update.updatedAt.seconds ||
        (job.updatedAt.seconds === update.updatedAt.seconds &&
          job.updatedAt.nanos > update.updatedAt.nanos)
      )
        return job;
    }
    return update;
  }

  start(basket: DownloadSelection) {
    const controller = new AbortController();
    const run = async () => {
      let delay = 1000;
      while (!controller.signal.aborted) {
        try {
          for await (const event of api.realtime.subscribe(
            {},
            { signal: controller.signal },
          )) {
            if (event.payload.case === 'ready') {
              this.connected = true;
              delay = 1000;
              basket.invalidateLookups();
              this.resetVersion++;
              this.libraryVersion++;
            } else if (event.payload.case === 'importUpdated') {
              const job = event.payload.value;
              this.imports = { ...this.imports, [job.id]: job };
              basket.updateJob(job);
            } else if (event.payload.case === 'libraryUpdated') {
              const match = event.payload.value.track;
              if (match) basket.markImported([match]);
              this.libraryVersion++;
            }
          }
        } catch (cause) {
          if (ConnectError.from(cause).code === Code.Unauthenticated) {
            session.expire();
            break;
          }
        } finally {
          this.connected = false;
        }
        if (controller.signal.aborted) break;
        await new Promise<void>((resolve) => {
          const done = () => {
            clearTimeout(timer);
            controller.signal.removeEventListener('abort', done);
            resolve();
          };
          const timer = setTimeout(done, delay);
          controller.signal.addEventListener('abort', done, { once: true });
        });
        delay = Math.min(delay * 2, 30000);
      }
    };
    void run();
    return () => controller.abort();
  }
}
