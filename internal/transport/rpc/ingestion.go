package rpc

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
)

type discoveryService struct{ discovery *discovery.Service }
type ingestionService struct{ ingest *ingest.Service }

func DiscoveryHandler(d *discovery.Service, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewDiscoveryServiceHandler(&discoveryService{d}, connect.WithReadMaxBytes(1<<20))
	return path, protect(handler, manager)
}
func IngestionHandler(s *ingest.Service, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewIngestionServiceHandler(&ingestionService{s}, connect.WithReadMaxBytes(1<<20))
	return path, protect(handler, manager)
}
func providerRef(r *pb.ProviderRef) discovery.Ref {
	if r == nil {
		return discovery.Ref{}
	}
	return discovery.Ref{Provider: r.Provider, ID: r.Id}
}
func refMessage(r discovery.Ref) *pb.ProviderRef {
	if r.Empty() {
		return nil
	}
	return &pb.ProviderRef{Provider: r.Provider, Id: r.ID}
}
func externalTrack(t discovery.Track) *pb.ExternalTrack {
	return &pb.ExternalTrack{Ref: refMessage(t.Ref), Title: t.Title, Artist: t.Artist, Album: t.Album, AlbumArtist: t.AlbumArtist, Genre: t.Genre, ReleaseDate: t.ReleaseDate, TrackNumber: int32(t.TrackNumber), DiscNumber: int32(t.DiscNumber), DurationSeconds: t.Duration, ArtworkUrl: t.ArtworkURL}
}
func musicRelease(r discovery.Release) *pb.MusicRelease {
	out := &pb.MusicRelease{Ref: refMessage(r.Ref), ArtistRef: refMessage(r.ArtistRef), Title: r.Title, Artist: r.Artist, ArtworkUrl: r.ArtworkURL}
	for _, t := range r.Tracks {
		out.Tracks = append(out.Tracks, externalTrack(t))
	}
	return out
}
func musicArtist(a discovery.Artist) *pb.MusicArtist {
	out := &pb.MusicArtist{Ref: refMessage(a.Ref), Name: a.Name, ArtworkUrl: a.ArtworkURL}
	for _, r := range a.Releases {
		out.Releases = append(out.Releases, musicRelease(r))
	}
	return out
}
func importJob(j ingest.Job) *pb.ImportJob {
	out := &pb.ImportJob{Id: j.ID, State: j.State, Error: j.Error, CreatedAt: timestamp(j.CreatedAt), UpdatedAt: timestamp(j.UpdatedAt), ReleaseRef: refMessage(j.Request.ReleaseRef), PlaylistId: j.Request.PlaylistID}
	for _, i := range j.Items {
		item := &pb.ImportItem{MetadataRef: refMessage(i.MetadataRef), SourceRef: refMessage(i.SourceRef), State: i.State, Error: i.Error, TrackId: i.TrackID, Reused: i.Reused, Warnings: i.Warnings}
		if i.Metadata != nil {
			item.Metadata = externalTrack(*i.Metadata)
		} else if i.Preview != nil {
			item.Metadata = externalTrack(*i.Preview)
		}
		item.AlbumId, item.ArtistId = i.AlbumID, i.ArtistID
		out.Items = append(out.Items, item)
	}
	return out
}
func (s *discoveryService) Search(ctx context.Context, r *connect.Request[pb.SearchMusicRequest]) (*connect.Response[pb.SearchMusicResponse], error) {
	if err := auth.RequireScope(ctx, auth.ScopeRead); err != nil {
		return nil, failure(err)
	}
	results, err := s.discovery.Search(ctx, r.Msg.Query, int(r.Msg.Limit))
	if err != nil {
		return nil, failure(err)
	}
	out := &pb.SearchMusicResponse{}
	for _, t := range results.Tracks {
		out.Tracks = append(out.Tracks, externalTrack(t))
	}
	for _, a := range results.Artists {
		out.Artists = append(out.Artists, musicArtist(a))
	}
	for _, a := range results.Releases {
		out.Releases = append(out.Releases, musicRelease(a))
	}
	return connect.NewResponse(out), nil
}
func (s *discoveryService) GetArtist(ctx context.Context, r *connect.Request[pb.GetMusicArtistRequest]) (*connect.Response[pb.GetMusicArtistResponse], error) {
	if err := auth.RequireScope(ctx, auth.ScopeRead); err != nil {
		return nil, failure(err)
	}
	a, err := s.discovery.Artist(ctx, providerRef(r.Msg.Ref))
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.GetMusicArtistResponse{Artist: musicArtist(a)}), nil
}
func (s *discoveryService) GetRelease(ctx context.Context, r *connect.Request[pb.GetMusicReleaseRequest]) (*connect.Response[pb.GetMusicReleaseResponse], error) {
	if err := auth.RequireScope(ctx, auth.ScopeRead); err != nil {
		return nil, failure(err)
	}
	a, err := s.discovery.Release(ctx, providerRef(r.Msg.Ref))
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.GetMusicReleaseResponse{Release: musicRelease(a)}), nil
}
func (s *discoveryService) FindSources(ctx context.Context, r *connect.Request[pb.FindSourcesRequest]) (*connect.Response[pb.FindSourcesResponse], error) {
	if err := auth.RequireScope(ctx, auth.ScopeRead); err != nil {
		return nil, failure(err)
	}
	candidates, err := s.discovery.FindSources(ctx, providerRef(r.Msg.MetadataRef))
	if err != nil {
		return nil, failure(err)
	}
	out := &pb.FindSourcesResponse{}
	for _, c := range candidates {
		out.Candidates = append(out.Candidates, &pb.SourceCandidate{Ref: refMessage(c.Ref), Title: c.Title, Uploader: c.Uploader, DurationSeconds: c.Duration, Score: c.Score, Suitable: c.Suitable, Reason: c.Reason})
	}
	return connect.NewResponse(out), nil
}

func (s *discoveryService) GetReleases(ctx context.Context, r *connect.Request[pb.GetMusicReleasesRequest]) (*connect.Response[pb.GetMusicReleasesResponse], error) {
	if err := auth.RequireScope(ctx, auth.ScopeRead); err != nil {
		return nil, failure(err)
	}
	refs := make([]discovery.Ref, 0, len(r.Msg.Refs))
	for _, ref := range r.Msg.Refs {
		refs = append(refs, providerRef(ref))
	}
	releases, err := s.discovery.Releases(ctx, refs)
	if err != nil {
		return nil, failure(err)
	}
	out := &pb.GetMusicReleasesResponse{}
	for _, release := range releases {
		out.Releases = append(out.Releases, musicRelease(release))
	}
	return connect.NewResponse(out), nil
}
func importUser(ctx context.Context, mutate bool) (string, error) {
	owner, err := auth.PersonalID(ctx, false)
	if err != nil {
		return "", err
	}
	if mutate {
		err = auth.RequireScope(ctx, auth.ScopeIngest)
	}
	return owner, err
}
func importResponse(j ingest.Job, err error) (*connect.Response[pb.ImportJob], error) {
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(importJob(j)), nil
}
func (s *ingestionService) LookupImports(ctx context.Context, r *connect.Request[pb.LookupImportsRequest]) (*connect.Response[pb.LookupImportsResponse], error) {
	if err := auth.RequireScope(ctx, auth.ScopeRead); err != nil {
		return nil, failure(err)
	}
	refs := make([]discovery.Ref, 0, len(r.Msg.MetadataRefs))
	for _, ref := range r.Msg.MetadataRefs {
		refs = append(refs, providerRef(ref))
	}
	found, err := s.ingest.Lookup(ctx, refs)
	if err != nil {
		return nil, failure(err)
	}
	origins, err := s.ingest.Origins(ctx, r.Msg.TrackIds)
	if err != nil {
		return nil, failure(err)
	}
	for ref, id := range origins {
		found[ref] = id
	}
	out := &pb.LookupImportsResponse{}
	for ref, id := range found {
		out.Tracks = append(out.Tracks, &pb.ImportedTrack{MetadataRef: refMessage(ref), TrackId: id})
	}
	return connect.NewResponse(out), nil
}

func (s *ingestionService) CreateImport(ctx context.Context, r *connect.Request[pb.CreateImportRequest]) (*connect.Response[pb.ImportJob], error) {
	owner, err := importUser(ctx, true)
	if err != nil {
		return nil, failure(err)
	}
	if r.Msg.PlaylistId != "" {
		if err = auth.RequireScope(ctx, auth.ScopeWrite); err != nil {
			return nil, failure(err)
		}
	}
	in := ingest.Request{RequestID: r.Msg.RequestId, ReleaseRef: providerRef(r.Msg.ReleaseRef), PlaylistID: r.Msg.PlaylistId}
	for _, i := range r.Msg.Items {
		in.Items = append(in.Items, ingest.Selection{MetadataRef: providerRef(i.GetMetadataRef()), SourceRef: providerRef(i.GetSourceRef()), ReplaceTrackID: i.GetReplaceTrackId()})
	}
	return importResponse(s.ingest.Create(ctx, owner, in))
}
func (s *ingestionService) GetImport(ctx context.Context, r *connect.Request[pb.GetImportRequest]) (*connect.Response[pb.ImportJob], error) {
	owner, err := importUser(ctx, false)
	if err != nil {
		return nil, failure(err)
	}
	return importResponse(s.ingest.Get(ctx, owner, r.Msg.Id))
}
func (s *ingestionService) ListImports(ctx context.Context, r *connect.Request[pb.ListImportsRequest]) (*connect.Response[pb.ListImportsResponse], error) {
	owner, err := importUser(ctx, false)
	if err != nil {
		return nil, failure(err)
	}
	jobs, total, err := s.ingest.List(ctx, owner, int(r.Msg.Limit), int(r.Msg.Offset))
	if err != nil {
		return nil, failure(err)
	}
	out := &pb.ListImportsResponse{Total: int32(total)}
	for _, j := range jobs {
		out.Imports = append(out.Imports, importJob(j))
	}
	return connect.NewResponse(out), nil
}
func (s *ingestionService) CancelImport(ctx context.Context, r *connect.Request[pb.CancelImportRequest]) (*connect.Response[pb.ImportJob], error) {
	owner, err := importUser(ctx, true)
	if err != nil {
		return nil, failure(err)
	}
	return importResponse(s.ingest.Cancel(ctx, owner, r.Msg.Id))
}
func (s *ingestionService) RetryImport(ctx context.Context, r *connect.Request[pb.RetryImportRequest]) (*connect.Response[pb.ImportJob], error) {
	owner, err := importUser(ctx, true)
	if err != nil {
		return nil, failure(err)
	}
	job, err := s.ingest.Get(ctx, owner, r.Msg.Id)
	if err != nil {
		return nil, failure(err)
	}
	if job.Request.PlaylistID != "" {
		if err = auth.RequireScope(ctx, auth.ScopeWrite); err != nil {
			return nil, failure(err)
		}
	}
	overrides := []ingest.Override{}
	for _, o := range r.Msg.Overrides {
		overrides = append(overrides, ingest.Override{Index: int(o.GetIndex()), SourceRef: providerRef(o.GetSourceRef())})
	}
	return importResponse(s.ingest.Retry(ctx, owner, r.Msg.Id, overrides))
}
