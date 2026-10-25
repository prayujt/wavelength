import type {
  ExternalTrack,
  ProviderRef,
  SourceCandidate,
  ImportedTrack,
  MusicRelease,
  ImportJob,
} from '../gen/wavelength/v1/ingestion_pb';
import { api } from './session.svelte';

export const refKey = (ref?: ProviderRef) =>
  ref ? `${ref.provider}:${ref.id}` : '';

export class DownloadSelection {
  tracks = $state<ExternalTrack[]>([]);
  sources = $state<Record<string, SourceCandidate>>({});
  imported = $state<Record<string, string>>({});
  releases = $state<Record<string, MusicRelease>>({});
  queued = $state<Record<string, string>>({});
  reviewing = $state(false);
  playlistId = $state('');
  private requestId = '';
  private importVersion = 0;
  private lookupGeneration = 0;
  private deletedIDs = new Set<string>();
  private lookedUp = new Set<string>();
  private downloadRequests = new Map<string, string>();
  duration = $derived(
    this.tracks.reduce((sum, track) => sum + track.durationSeconds, 0),
  );

  has(track: ExternalTrack) {
    return this.tracks.some((item) => refKey(item.ref) === refKey(track.ref));
  }
  localID(track: ExternalTrack) {
    return this.imported[refKey(track.ref)] || '';
  }
  add(tracks: ExternalTrack[]) {
    this.tracks = [
      ...new Map(
        [...this.tracks, ...tracks.filter((track) => !this.localID(track))].map(
          (track) => [refKey(track.ref), track],
        ),
      ).values(),
    ];
    this.requestId = '';
  }
  remove(track: ExternalTrack) {
    this.tracks = this.tracks.filter(
      (item) => refKey(item.ref) !== refKey(track.ref),
    );
    this.requestId = '';
  }
  toggle(track: ExternalTrack) {
    if (this.has(track)) this.remove(track);
    else this.add([track]);
  }
  choose(track: ExternalTrack, source: SourceCandidate) {
    this.sources = { ...this.sources, [refKey(track.ref)]: source };
    this.add([track]);
  }
  automatic(track: ExternalTrack) {
    const sources = { ...this.sources };
    delete sources[refKey(track.ref)];
    this.sources = sources;
    this.requestId = '';
  }
  playlist(id: string) {
    this.playlistId = id;
    this.requestId = '';
  }
  clear() {
    this.tracks = [];
    this.sources = {};
    this.playlistId = '';
    this.requestId = '';
    this.reviewing = false;
  }

  forgetTrack(id: string) {
    this.deletedIDs.add(id);
    this.imported = Object.fromEntries(
      Object.entries(this.imported).filter(([, localID]) => localID !== id),
    );
    this.importVersion++;
    this.invalidateLookups();
  }
  invalidateLookups() {
    this.lookedUp.clear();
    this.lookupGeneration++;
  }

  async lookup(tracks: ExternalTrack[], signal?: AbortSignal, refresh = false) {
    if (!tracks.length) return;
    if (tracks.length > 200) {
      for (let offset = 0; offset < tracks.length; offset += 200)
        await this.lookup(tracks.slice(offset, offset + 200), signal, refresh);
      return;
    }
    const metadataRefs = [
      ...new Map(
        tracks.flatMap((track) =>
          track.ref && (refresh || !this.lookedUp.has(refKey(track.ref)))
            ? [[refKey(track.ref), track.ref] as const]
            : [],
        ),
      ).values(),
    ];
    if (!metadataRefs.length) return;
    const version = this.importVersion;
    const generation = this.lookupGeneration;
    const result = await api.ingestion.lookupImports(
      { metadataRefs },
      { signal },
    );
    if (signal?.aborted) return;
    const imported = { ...this.imported };
    if (version === this.importVersion)
      for (const ref of metadataRefs) delete imported[refKey(ref)];
    this.imported = imported;
    this.markImported(result.tracks);
    if (generation === this.lookupGeneration)
      for (const ref of metadataRefs) this.lookedUp.add(refKey(ref));
  }

  release(album: MusicRelease) {
    return (
      this.releases[refKey(album.ref)] ||
      (album.tracks.length ? album : undefined)
    );
  }

  async lookupReleases(albums: MusicRelease[], signal?: AbortSignal) {
    const missing = albums.filter((album) => !this.release(album));
    for (let offset = 0; offset < missing.length; offset += 20) {
      const result = await api.discovery.getReleases(
        {
          refs: missing
            .slice(offset, offset + 20)
            .flatMap((album) => (album.ref ? [album.ref] : [])),
        },
        { signal },
      );
      if (signal?.aborted) return;
      this.releases = {
        ...this.releases,
        ...Object.fromEntries(
          result.releases.map((album) => [refKey(album.ref), album]),
        ),
      };
    }
    await this.lookup(
      albums.flatMap((album) => this.release(album)?.tracks || []),
      signal,
    );
  }

  async download(tracks: ExternalTrack[]) {
    const pending = tracks.filter(
      (track) => !this.localID(track) && !this.queued[refKey(track.ref)],
    );
    if (!pending.length) return;
    const key = JSON.stringify(pending.map((track) => refKey(track.ref)));
    const requestId = this.downloadRequests.get(key) || crypto.randomUUID();
    this.downloadRequests.set(key, requestId);
    this.queued = {
      ...this.queued,
      ...Object.fromEntries(
        pending.map((track) => [refKey(track.ref), requestId]),
      ),
    };
    try {
      const job = await api.ingestion.createImport({
        requestId,
        items: pending.map((track) => ({ metadataRef: track.ref })),
      });
      this.queued = {
        ...this.queued,
        ...Object.fromEntries(
          pending.map((track) => [refKey(track.ref), job.id]),
        ),
      };
      this.downloadRequests.delete(key);
      const submitted = new Set(pending.map((track) => refKey(track.ref)));
      this.tracks = this.tracks.filter(
        (track) => !submitted.has(refKey(track.ref)),
      );
      this.requestId = '';
      return job;
    } catch (cause) {
      const queued = { ...this.queued };
      for (const track of pending) delete queued[refKey(track.ref)];
      this.queued = queued;
      throw cause;
    }
  }

  markImported(matches: ImportedTrack[]) {
    this.importVersion++;
    const imported = { ...this.imported };
    for (const match of matches)
      if (!this.deletedIDs.has(match.trackId))
        imported[refKey(match.metadataRef)] = match.trackId;
    this.imported = imported;
    const remaining = this.tracks.filter((track) => !this.localID(track));
    if (remaining.length !== this.tracks.length) {
      this.tracks = remaining;
      this.requestId = '';
    }
  }

  updateJob(job: ImportJob) {
    const queued = { ...this.queued };
    for (const item of job.items) {
      const key = refKey(item.metadataRef);
      if (
        ['queued', 'running', 'needs_selection'].includes(job.state) &&
        item.state !== 'completed'
      )
        queued[key] = job.id;
      else if (queued[key] === job.id) delete queued[key];
    }
    this.queued = queued;
  }

  request() {
    this.requestId ||= crypto.randomUUID();
    return {
      requestId: this.requestId,
      playlistId: this.playlistId,
      items: this.tracks.map((track) => ({
        metadataRef: track.ref,
        sourceRef: this.sources[refKey(track.ref)]?.ref,
      })),
    };
  }
}
