import type {
  ExternalTrack,
  ProviderRef,
  SourceCandidate,
} from '../gen/wavelength/v1/ingestion_pb';
import { api } from './session.svelte';

export const refKey = (ref?: ProviderRef) =>
  ref ? `${ref.provider}:${ref.id}` : '';

export class DownloadSelection {
  tracks = $state<ExternalTrack[]>([]);
  sources = $state<Record<string, SourceCandidate>>({});
  imported = $state<Record<string, string>>({});
  reviewing = $state(false);
  playlistId = $state('');
  private requestId = '';
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

  async lookup(tracks: ExternalTrack[], signal?: AbortSignal) {
    if (!tracks.length) return;
    const metadataRefs = tracks.flatMap((track) =>
      track.ref ? [track.ref] : [],
    );
    const result = await api.ingestion.lookupImports(
      { metadataRefs },
      { signal },
    );
    if (signal?.aborted) return;
    const imported = { ...this.imported };
    for (const ref of metadataRefs) delete imported[refKey(ref)];
    for (const match of result.tracks)
      imported[refKey(match.metadataRef)] = match.trackId;
    this.imported = imported;
    const remaining = this.tracks.filter((track) => !this.localID(track));
    if (remaining.length !== this.tracks.length) {
      this.tracks = remaining;
      this.requestId = '';
    }
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
