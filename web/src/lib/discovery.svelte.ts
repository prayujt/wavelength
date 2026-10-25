import type { SearchMusicResponse } from '../gen/wavelength/v1/ingestion_pb';

// Keep the last result set while browsing its artists and albums.
export class DiscoveryState {
  query = $state('');
  searched = $state('');
  category = $state('all');
  results = $state<SearchMusicResponse>();
  scroll = 0;
}
